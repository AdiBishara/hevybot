package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
	apiKey        string
}

// NewTelegramHandler constructs a TelegramHandler with its dependencies.
func NewTelegramHandler(logger *slog.Logger, webhookSecret string, tgClient telegram.Client, dbStore db.Store, aiClient ai.Client, apiKey string) *TelegramHandler {
	return &TelegramHandler{
		logger:        logger,
		webhookSecret: webhookSecret,
		tgClient:      tgClient,
		dbStore:       dbStore,
		aiClient:      aiClient,
		apiKey:        apiKey,
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

	// Handle Callback Queries (Button clicks)
	if update.CallbackQuery != nil {
		ctx := context.Background()
		go h.handleCallbackQuery(ctx, update.CallbackQuery)
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message == nil {
		// Telegram sends non-message updates (e.g., edited_message).
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("telegram: received message",
		"update_id", update.UpdateID,
		"chat_id", update.Message.Chat.ID,
		"text", update.Message.Text,
	)

	// ── Phase 2/3/4/5: Route commands & callbacks ──
	ctx := context.Background() // Use background context for async processing if needed, or request context

	text := strings.TrimSpace(update.Message.Text)
	chatID := update.Message.Chat.ID

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

		case strings.HasPrefix(cmd, "/backfill"):
			h.tgClient.SendMessage(ctx, chat, "⏳ Starting historical backfill... this might take a minute.")
			client := &http.Client{Timeout: 10 * time.Second}
			page := 1
			totalSynced := 0
			
			// Simple paginated response struct for Hevy
			type paginatedResponse struct {
				Workouts []models.HevyWorkout `json:"workouts"`
			}
			
			for {
				url := fmt.Sprintf("https://api.hevyapp.com/v1/workouts?page=%d&pageSize=10", page)
				req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
				req.Header.Set("api-key", h.apiKey)
				req.Header.Set("accept", "application/json")
				
				resp, err := client.Do(req)
				if err != nil || resp.StatusCode != http.StatusOK {
					h.logger.Error("backfill api failed", "error", err)
					break
				}
				
				var payload paginatedResponse
				json.NewDecoder(resp.Body).Decode(&payload)
				resp.Body.Close()
				
				if len(payload.Workouts) == 0 {
					break
				}
				
				for _, w := range payload.Workouts {
					if err := h.dbStore.SaveWorkout(ctx, &w); err == nil {
						totalSynced++
					}
				}
				
				page++
				time.Sleep(1 * time.Second) // rate limit
			}
			h.tgClient.SendMessage(ctx, chat, fmt.Sprintf("✅ Backfill complete! Synced %d historical workouts.", totalSynced))

		case strings.HasPrefix(cmd, "/lastworkout"):
			stats, err := h.dbStore.GetLastWorkoutDetailed(ctx)
			if err != nil {
				h.logger.Error("failed to get last workout", "error", err)
				h.tgClient.SendMessage(ctx, chat, "Sorry, I couldn't fetch your last workout.")
				return
			}
			if stats == nil {
				h.tgClient.SendMessage(ctx, chat, "You haven't logged any workouts yet!")
				return
			}

			var b strings.Builder
			b.WriteString(fmt.Sprintf("🏋️‍♂️ <b>Last Workout: %s</b>\n", stats.Title))
			b.WriteString(fmt.Sprintf("📅 Date: %s\n", stats.StartTime))
			b.WriteString(fmt.Sprintf("📊 Volume: <b>%.1f kg</b>\n", stats.Volume))
			b.WriteString(fmt.Sprintf("🔢 Sets: %d\n\n", stats.Sets))
			b.WriteString("📝 <b>Exercises Performed:</b>\n")
			for _, ex := range stats.Exercises {
				b.WriteString(fmt.Sprintf("- %s\n", ex))
			}

			h.tgClient.SendMessage(ctx, chat, b.String())

		case strings.HasPrefix(cmd, "/musclegroup"):
			keyboard := map[string]interface{}{
				"inline_keyboard": [][]map[string]string{
					{
						{"text": "Chest", "callback_data": "muscle:Chest"},
						{"text": "Back", "callback_data": "muscle:Back"},
					},
					{
						{"text": "Legs", "callback_data": "muscle:Legs"},
						{"text": "Arms", "callback_data": "muscle:Arms"},
					},
					{
						{"text": "Shoulders", "callback_data": "muscle:Shoulders"},
						{"text": "Core", "callback_data": "muscle:Core"},
					},
				},
			}
			h.tgClient.SendKeyboard(ctx, chat, "Select a muscle group to view your total lifetime volume:", keyboard)

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

func (h *TelegramHandler) handleCallbackQuery(ctx context.Context, cb *models.TelegramCallbackQuery) {
	h.logger.Info("processing callback query", "callback_id", cb.ID, "data", cb.Data)

	if cb.From == nil {
		h.logger.Error("callback query missing From field")
		return
	}

	// Stop the loading spinner on the user's phone
	if err := h.tgClient.AnswerCallbackQuery(ctx, cb.ID); err != nil {
		h.logger.Error("failed to answer callback query", "error", err)
	}

	// Always safely use From.ID since the original Message might be nil
	chatID := cb.From.ID

	if strings.HasPrefix(cb.Data, "muscle:") {
		muscle := strings.TrimPrefix(cb.Data, "muscle:")
		rms, err := h.dbStore.GetMuscleGroup1RM(ctx, muscle)
		if err != nil {
			h.logger.Error("failed to get muscle 1rm", "muscle", muscle, "error", err)
			h.tgClient.SendMessage(ctx, chatID, fmt.Sprintf("Sorry, I couldn't calculate the 1RM for %s right now.", muscle))
			return
		}

		if len(rms) == 0 {
			h.tgClient.SendMessage(ctx, chatID, fmt.Sprintf("💪 <b>%s 1RM Records</b>\n\nYou haven't logged any %s exercises yet!", muscle, muscle))
			return
		}

		var b strings.Builder
		b.WriteString(fmt.Sprintf("💪 <b>%s 1RM Records</b>\n\n", muscle))
		for _, rm := range rms {
			b.WriteString(fmt.Sprintf("• %s: <b>%.1f kg</b> (1RM) | <b>%.1f kg</b> (Max)\n", rm.Title, rm.OneRM, rm.MaxWeight))
		}

		h.tgClient.SendMessage(ctx, chatID, b.String())
	}
}
