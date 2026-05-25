# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependency downloads separately from source changes
COPY go.mod go.sum* ./
RUN go mod download

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
