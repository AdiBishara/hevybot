package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/yourusername/hevybot/internal/models"
)

// TelegramHandler groups all dependencies needed by Telegram webhook handlers.
type TelegramHandler struct {
	logger         *slog.Logger
	botToken       string
	webhookSecret  string
	// db    db.Store      ← injected in Phase 2
	// ai    ai.Client     ← injected in Phase 3
}

// NewTelegramHandler constructs a TelegramHandler with its dependencies.
func NewTelegramHandler(logger *slog.Logger, botToken, webhookSecret string) *TelegramHandler {
	return &TelegramHandler{
		logger:        logger,
		botToken:      botToken,
		webhookSecret: webhookSecret,
	}
}

// HandleUpdate handles POST /webhooks/telegram.
// Telegram POSTs a TelegramUpdate for every message, command, or callback.
//
// Phase 1 (current): validate shape, log, return 200.
// Phase 2: route commands (/stats, /lastworkout) against Turso data.
// Phase 3: forward free-text questions to Gemini.
func (h *TelegramHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	// ── Secret-token validation (Telegram sends X-Telegram-Bot-Api-Secret-Token header) ──
	if h.webhookSecret != "" {
		received := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if received != h.webhookSecret {
			h.logger.Warn("telegram: invalid webhook secret token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var update models.TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		h.logger.Error("telegram: failed to decode update", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if update.Message == nil {
		// Telegram sends non-message updates (e.g., edited_message, callback_query).
		// Acknowledge silently — routing for these types added in Phase 4.
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("telegram: received message",
		"update_id", update.UpdateID,
		"chat_id", update.Message.Chat.ID,
		"text", update.Message.Text,
	)

	// ── Phase 2: route command against Turso ──
	// ── Phase 3: forward free text to Gemini ──
	// ── Phase 4: reply via Telegram sendMessage API ──

	w.WriteHeader(http.StatusOK)
}
