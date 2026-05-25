package config

import (
	"fmt"
	"os"
)

// Config holds all runtime secrets and settings loaded from environment variables.
// Never hardcode values here — always inject via Koyeb's env var UI or a local .env loader.
type Config struct {
	Port              string // HTTP port the server listens on (default: 8080)
	HevyWebhookSecret string // Shared secret used to validate inbound Hevy webhook signatures
	HevyAPIKey        string // Used to fetch the full workout after receiving a ping
	TelegramBotToken  string // Token issued by @BotFather
	TelegramWebhookSecret string // Optional secret header Telegram sends with each update
	TursoDBURL        string // wss://your-db.turso.io
	TursoAuthToken    string // JWT issued by Turso
	GeminiAPIKey      string // Key from Google AI Studio
}

// Load reads required environment variables and returns a validated Config.
// The application will fail fast on startup if any required value is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		HevyWebhookSecret:    requireEnv("HEVY_WEBHOOK_SECRET"),
		HevyAPIKey:           requireEnv("HEVY_API_KEY"),
		TelegramBotToken:     requireEnv("TELEGRAM_BOT_TOKEN"),
		TelegramWebhookSecret: getEnv("TELEGRAM_WEBHOOK_SECRET", ""),
		TursoDBURL:           requireEnv("TURSO_DB_URL"),
		TursoAuthToken:       requireEnv("TURSO_AUTH_TOKEN"),
		GeminiAPIKey:         requireEnv("GEMINI_API_KEY"),
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
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("config: required environment variable %q is not set", key))
	}
	return v
}

// getEnv fetches an env var with a fallback default value.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
