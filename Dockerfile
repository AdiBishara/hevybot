# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git (go mod needs it to fetch modules from GitHub)
# Install ca-certificates so HTTPS fetches work during build
RUN apk add --no-cache git ca-certificates

# Copy ALL source first — go mod tidy needs to scan imports to know
# which dependencies are actually used before it can fetch them.
COPY . .

# Fetch dependencies and generate go.sum in one step
RUN go mod tidy

# Compile a statically-linked binary (no libc dependency)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bot ./cmd/bot

# ── Stage 2: Minimal runtime image ──────────────────────────────────────────
FROM scratch

# CA certs are required at runtime for HTTPS calls to Turso, Telegram, Gemini
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# The compiled binary is the only thing that runs in production
COPY --from=builder /bot /bot

EXPOSE 8080

ENTRYPOINT ["/bot"]
