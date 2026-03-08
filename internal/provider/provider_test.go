package provider

import (
	"testing"
)

func TestMakeSourceTag(t *testing.T) {
	tag := MakeSourceTag("google-personal", "primary", "event123")
	want := "google-personal:primary:event123"
	if tag != want {
		t.Errorf("MakeSourceTag = %q, want %q", tag, want)
	}
}

func TestEventIsBlocker(t *testing.T) {
	tests := []struct {
		name      string
		sourceTag string
		want      bool
	}{
		{"real event", "", false},
		{"blocker event", "cal1:primary:ev1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := Event{SourceTag: tt.sourceTag}
			if got := ev.IsBlocker(); got != tt.want {
				t.Errorf("IsBlocker() = %v, want %v", got, tt.want)
			}
		})
	}
}
