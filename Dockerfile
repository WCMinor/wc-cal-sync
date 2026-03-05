FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /wc-cal-sync ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /wc-cal-sync /usr/local/bin/wc-cal-sync

# Create directories for tokens and state
RUN mkdir -p /data /tokens

EXPOSE 8080
ENTRYPOINT ["wc-cal-sync"]
CMD ["-config", "/config/config.yaml"]
