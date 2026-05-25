package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yourusername/hevybot/internal/ai"
	"github.com/yourusername/hevybot/internal/db"
	"github.com/yourusername/hevybot/internal/models"
	"github.com/yourusername/hevybot/internal/telegram"
)

// TelegramHandler groups all dependencies needed by Telegram webhook handlers.
type TelegramHandler struct {
	logger        *slog.Logger
	webhookSecret string
	tgClient      telegram.Client
	dbStore       db.Store
	aiClient      ai.Client
}

// NewTelegramHandler constructs a TelegramHandler with its dependencies.
func NewTelegramHandler(logger *slog.Logger, webhookSecret string, tgClient telegram.Client, dbStore db.Store, aiClient ai.Client) *TelegramHandler {
	return &TelegramHandler{
		logger:        logger,
		webhookSecret: webhookSecret,
		tgClient:      tgClient,
		dbStore:       dbStore,
		aiClient:      aiClient,
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

	// ── Phase 2/3/4: Route commands ──
	text := strings.TrimSpace(update.Message.Text)
	chatID := update.Message.Chat.ID
	ctx := context.Background() // Use background context for async processing if needed, or request context

	// If no text (e.g. sticker, photo), ignore
	if text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	go func(chat int64, cmd string) {
		switch {
		case strings.HasPrefix(cmd, "/start"):
			h.tgClient.SendMessage(ctx, chat, "Welcome to HevyBot! Your workout data is actively syncing.\n\nTry /stats or /lastworkout, or ask me a fitness question!")

		case strings.HasPrefix(cmd, "/stats"):
			totalWorkouts, totalWeight, err := h.dbStore.GetStats(ctx)
			if err != nil {
				h.logger.Error("failed to get stats", "error", err)
				h.tgClient.SendMessage(ctx, chat, "Sorry, I couldn't fetch your stats right now.")
				return
			}
			msg := fmt.Sprintf("📊 <b>Lifetime Stats</b>\n\nTotal Workouts: <b>%d</b>\nTotal Volume: <b>%.1f kg</b>", totalWorkouts, totalWeight)
			h.tgClient.SendMessage(ctx, chat, msg)

		case strings.HasPrefix(cmd, "/lastworkout"):
			title, startTime, err := h.dbStore.GetLastWorkout(ctx)
			if err != nil {
				h.logger.Error("failed to get last workout", "error", err)
				h.tgClient.SendMessage(ctx, chat, "Sorry, I couldn't fetch your last workout.")
				return
			}
			if title == "" {
				h.tgClient.SendMessage(ctx, chat, "You haven't logged any workouts yet!")
				return
			}
			msg := fmt.Sprintf("🏋️‍♂️ <b>Last Workout</b>\n\n<b>%s</b>\nDate: %s", title, startTime)
			h.tgClient.SendMessage(ctx, chat, msg)

		default:
			// Free text question: Let's create a "dummy" HevyWorkout that just contains their question as the Title
			// Alternatively, since our Gemini client expects a HevyWorkout, we can just pass a dummy one for now.
			// Actually, let's just let the AI know it's a direct question.
			dummy := &models.HevyWorkout{
				Title: fmt.Sprintf("User Question: %s", cmd),
			}
			
			feedback, err := h.aiClient.GenerateCoachingFeedback(ctx, dummy)
			if err != nil {
				h.logger.Error("failed to generate answer from gemini", "error", err)
				h.tgClient.SendMessage(ctx, chat, "I'm having trouble thinking right now. Try again later.")
				return
			}
			h.tgClient.SendMessage(ctx, chat, feedback)
		}
	}(chatID, text)

	w.WriteHeader(http.StatusOK)
}
