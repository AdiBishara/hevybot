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
			stats, err := h.dbStore.GetStats(ctx)
			if err != nil {
				h.logger.Error("failed to get stats", "error", err)
				h.tgClient.SendMessage(ctx, chat, "Sorry, I couldn't fetch your stats right now.")
				return
			}
			
			var b strings.Builder
			b.WriteString(fmt.Sprintf("📊 <b>Lifetime Stats</b>\n\nTotal Workouts: <b>%d</b>\nTotal Volume: <b>%.1f kg</b>\n\n", stats.TotalWorkouts, stats.TotalVolume))
			b.WriteString("💪 <b>Muscle Group Training Frequency:</b>\n")
			for muscle, count := range stats.MuscleCounts {
				if count > 0 {
					b.WriteString(fmt.Sprintf("- %s: %d exercises logged\n", muscle, count))
				}
			}
			h.tgClient.SendMessage(ctx, chat, b.String())

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

			// Generate AI Suggestion for the next workout
			aiPrompt := fmt.Sprintf("The user just requested their last workout stats. Here they are:\nVolume: %.1f kg\nSets: %d\nExercises: %s\n\nWrite a highly specific 2-sentence motivational suggestion for their NEXT workout. You MUST analyze the specific weights and reps they performed, and explicitly suggest an exact number of kilograms to add or exact rep range to aim for in their next session based on standard progressive overload principles. Be concise, mathematical, and conversational.", stats.Volume, stats.Sets, strings.Join(stats.Exercises, ", "))
			suggestion, aiErr := h.aiClient.Chat(ctx, aiPrompt)
			if aiErr == nil && suggestion != "" {
				b.WriteString("\n🤖 <b>Coach's Suggestion for Next Time:</b>\n")
				b.WriteString(suggestion)
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
			h.logger.Info("routing free text to gemini with context injection", "text", cmd)

			// Fetch recent workouts for context injection
			recentWorkouts, err := h.dbStore.GetRecentWorkoutsDetailed(ctx, 10)
			if err != nil {
				h.logger.Error("failed to fetch recent workouts for context", "error", err)
			}

			var prompt strings.Builder
			prompt.WriteString("You are a highly advanced fitness AI coach. The user is asking a fitness question, requesting an 'audible' (a routine tweak), or asking you to generate a completely new custom workout routine. ")
			prompt.WriteString("To help you give highly personalized advice, here is their recent workout history, including the heaviest weight they lifted for each exercise:\n\n")

			if len(recentWorkouts) > 0 {
				for _, w := range recentWorkouts {
					prompt.WriteString(fmt.Sprintf("- %s (%s): %s\n", w.Title, w.StartTime, strings.Join(w.Exercises, ", ")))
				}
			} else {
				prompt.WriteString("(No recent workouts found in database)\n")
			}

			prompt.WriteString("\nCRITICAL INSTRUCTIONS:\n")
			prompt.WriteString("1. If the user is asking you to generate a completely new workout routine, you MUST generate a beautifully formatted day-by-day structured plan (e.g. Day 1: Push, Day 2: Pull, etc.). You MUST suggest specific starting weights for them by mathematically estimating it based on their max weights from their history.\n")
			prompt.WriteString("2. If suggesting a replacement exercise for an existing routine, you MUST explicitly state whether your suggested replacement is a 'Compound' or 'Isolation' movement, and you MUST suggest a specific starting working weight.\n\n")

			prompt.WriteString("User's Message: " + cmd)

			response, err := h.aiClient.Chat(ctx, prompt.String())
			if err != nil {
				h.logger.Error("failed to generate answer from gemini", "error", err)
				h.tgClient.SendMessage(ctx, chat, "I'm having trouble thinking right now. Try again later.")
				return
			}
			h.tgClient.SendMessage(ctx, chat, response)
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
