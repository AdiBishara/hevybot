# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git (needed by go mod to fetch from GitHub)
RUN apk add --no-cache git ca-certificates

# Copy module definition — run tidy to fetch deps and generate go.sum on the fly
COPY go.mod ./
RUN go mod tidy

# Copy source and build a statically-linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bot ./cmd/bot

# ── Stage 2: Minimal runtime image ──────────────────────────────────────────
FROM scratch

# Copy CA certificates so HTTPS calls to Turso/Gemini/Telegram work
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary
COPY --from=builder /bot /bot

EXPOSE 8080

ENTRYPOINT ["/bot"]
