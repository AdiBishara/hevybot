package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yourusername/hevybot/internal/ai"
	"github.com/yourusername/hevybot/internal/config"
	"github.com/yourusername/hevybot/internal/db"
	"github.com/yourusername/hevybot/internal/handlers"
	"github.com/yourusername/hevybot/internal/models"
	"github.com/yourusername/hevybot/internal/telegram"
)

func main() {
	// ── Structured logger (JSON in production, text locally via LOG_FORMAT=text) ──
	logger := newLogger()

	// ── Load and validate configuration from environment ──
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// ── Database Init & Migrations ──
	if err := db.RunMigrations(context.Background(), cfg.TursoDBURL, cfg.TursoAuthToken); err != nil {
		logger.Error("failed to run database migrations", "error", err)
		os.Exit(1)
	}
	store, err := db.NewTursoStore(cfg.TursoDBURL, cfg.TursoAuthToken)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to turso database successfully")

	// ── AI & Telegram Clients ──
	aiClient := ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel)
	tgClient := telegram.NewTelegramClient(cfg.TelegramBotToken)

	err = tgClient.SetMyCommands(context.Background(), []models.BotCommand{
		{Command: "start", Description: "Start the bot and sync history"},
		{Command: "stats", Description: "View your lifetime workout stats"},
		{Command: "lastworkout", Description: "View detailed stats of your last workout"},
		{Command: "musclegroup", Description: "Select a muscle to view your 1RM records"},
	})
	if err != nil {
		logger.Warn("failed to set telegram menu commands", "error", err)
	}

	// ── Instantiate handlers (inject dependencies as they are added per phase) ──
	hevyH := handlers.NewHevyHandler(logger, cfg.HevyWebhookSecret, cfg.HevyAPIKey, store, aiClient, tgClient, cfg.TelegramChatID)
	telegramH := handlers.NewTelegramHandler(logger, cfg.TelegramWebhookSecret, tgClient, store, aiClient, cfg.HevyAPIKey)

	// ── Router ──
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)        // Injects X-Request-Id for tracing
	r.Use(middleware.RealIP)           // Reads X-Forwarded-For (set by Koyeb's proxy)
	r.Use(middleware.Logger)           // Structured access log per request
	r.Use(middleware.Recoverer)        // Catches panics, returns 500, logs stack trace
	r.Use(middleware.Timeout(10 * time.Second)) // Hard deadline per request

	// Health-check — required by Koyeb's readiness probe
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Webhook endpoints
	r.Post("/webhooks/hevy", hevyH.HandleWorkoutEvent)
	r.Post("/webhooks/telegram", telegramH.HandleUpdate)

	// ── HTTP server with graceful shutdown ──
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start in background goroutine
	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Block until SIGINT or SIGTERM (Koyeb sends SIGTERM on scale-to-zero)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutdown signal received — draining connections")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped cleanly")
}

// newLogger returns a JSON logger for production and a human-readable one locally.
func newLogger() *slog.Logger {
	if os.Getenv("LOG_FORMAT") == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}
