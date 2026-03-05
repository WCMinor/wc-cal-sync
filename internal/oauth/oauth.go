package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// TokenFromFile loads a saved OAuth2 token from disk.
func TokenFromFile(path string) (*oauth2.Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parsing token file %s: %w", path, err)
	}
	return &tok, nil
}

// SaveToken persists an OAuth2 token to disk.
func SaveToken(path string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// InteractiveFlow runs a local OAuth2 authorization code flow.
// It starts a temporary HTTP server to receive the callback.
func InteractiveFlow(ctx context.Context, cfg *oauth2.Config, port int) (*oauth2.Token, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	state := fmt.Sprintf("wc-cal-sync-%d", time.Now().UnixNano())

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("oauth error: %s: %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "no code", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "<html><body><h1>Authorization successful!</h1><p>You can close this window.</p></body></html>")
		codeCh <- code
	})

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{Addr: addr, Handler: mux}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	cfg.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	log.Printf("Open this URL in your browser to authorize:\n\n%s\n", authURL)

	var token *oauth2.Token
	select {
	case code := <-codeCh:
		var err error
		token, err = cfg.Exchange(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("exchanging code: %w", err)
		}
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(shutdownCtx)
	wg.Wait()

	return token, nil
}

// GetClient returns an HTTP client with a valid OAuth2 token.
// It loads the token from tokenFile, refreshes if needed, and saves back.
// If no token file exists, it runs the interactive flow.
func GetClient(ctx context.Context, cfg *oauth2.Config, tokenFile string, callbackPort int) (*http.Client, error) {
	tok, err := TokenFromFile(tokenFile)
	if err != nil {
		// No saved token — run interactive flow
		log.Printf("No saved token at %s, starting OAuth flow...", tokenFile)
		tok, err = InteractiveFlow(ctx, cfg, callbackPort)
		if err != nil {
			return nil, fmt.Errorf("interactive oauth: %w", err)
		}
		if err := SaveToken(tokenFile, tok); err != nil {
			return nil, fmt.Errorf("saving token: %w", err)
		}
	}

	// Create a token source that auto-refreshes
	ts := cfg.TokenSource(ctx, tok)

	// Check if the token was refreshed and save it
	newTok, err := ts.Token()
	if err != nil {
		// Token might be expired and refresh failed — re-auth
		log.Printf("Token refresh failed: %v. Starting OAuth flow...", err)
		tok, err = InteractiveFlow(ctx, cfg, callbackPort)
		if err != nil {
			return nil, fmt.Errorf("re-auth oauth: %w", err)
		}
		if err := SaveToken(tokenFile, tok); err != nil {
			return nil, fmt.Errorf("saving token: %w", err)
		}
		ts = cfg.TokenSource(ctx, tok)
	} else if newTok.AccessToken != tok.AccessToken {
		// Token was refreshed, save the new one
		if err := SaveToken(tokenFile, newTok); err != nil {
			log.Printf("Warning: failed to save refreshed token: %v", err)
		}
	}

	return oauth2.NewClient(ctx, ts), nil
}
