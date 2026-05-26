package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime secrets and settings loaded from environment variables.
// Never hardcode values here — always inject via Koyeb's env var UI or a local .env loader.
type Config struct {
	Port              string // HTTP port the server listens on (default: 8080)
	HevyWebhookSecret string // Shared secret used to validate inbound Hevy webhook signatures
	HevyAPIKey        string // Used to fetch the full workout after receiving a ping
	TelegramBotToken      string // Token issued by @BotFather
	TelegramWebhookSecret string // Optional secret header Telegram sends with each update
	TelegramChatID        int64  // The specific chat ID to send autonomous AI feedback to
	TursoDBURL        string // wss://your-db.turso.io
	TursoAuthToken    string // JWT issued by Turso
	GeminiAPIKey      string // Key from Google AI Studio
	GeminiModel       string // e.g., gemini-1.5-pro
}

// Load reads required environment variables and returns a validated Config.
// The application will fail fast on startup if any required value is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		HevyWebhookSecret:    getEnv("HEVY_WEBHOOK_SECRET", ""),
		HevyAPIKey:           getEnv("HEVY_API_KEY", ""),
		TelegramBotToken:     requireEnv("TELEGRAM_BOT_TOKEN"),
		TelegramWebhookSecret: getEnv("TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramChatID:       requireEnvInt64("TELEGRAM_CHAT_ID"),
		TursoDBURL:           requireEnv("TURSO_DB_URL"),
		TursoAuthToken:       requireEnv("TURSO_AUTH_TOKEN"),
		GeminiAPIKey:         requireEnv("GEMINI_API_KEY"),
		GeminiModel:          getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
	}

	// Validate PORT is a non-empty string (Koyeb injects this automatically)
	if cfg.Port == "" {
		return nil, fmt.Errorf("config: PORT must not be empty")
	}

	return cfg, nil
}

// requireEnv fetches an env var and panics with a clear message if it is not set.
// Fail-fast is intentional: a misconfigured pod should never silently handle requests.
func requireEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		panic(fmt.Sprintf("config: required environment variable %q is not set", key))
	}
	return v
}

// getEnv fetches an env var with a fallback default value.
func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// requireEnvInt64 fetches an env var and parses it as int64, panicking if missing or invalid.
func requireEnvInt64(key string) int64 {
	v := requireEnv(key)
	var i int64
	_, err := fmt.Sscanf(v, "%d", &i)
	if err != nil {
		panic(fmt.Sprintf("config: environment variable %q must be a valid integer", key))
	}
	return i
}
