package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yourusername/hevybot/internal/ai"
	"github.com/yourusername/hevybot/internal/db"
	"github.com/yourusername/hevybot/internal/models"
	"github.com/yourusername/hevybot/internal/telegram"
)

// HevyHandler groups all dependencies needed by Hevy webhook handlers.
// Unexported fields are injected via NewHevyHandler — no globals.
type HevyHandler struct {
	logger        *slog.Logger
	webhookSecret string
	apiKey        string
	dbStore       db.Store
	aiClient      ai.Client
	tgClient      telegram.Client
	chatID        int64
}

// NewHevyHandler constructs a HevyHandler with its dependencies.
func NewHevyHandler(logger *slog.Logger, webhookSecret, apiKey string, dbStore db.Store, aiClient ai.Client, tgClient telegram.Client, chatID int64) *HevyHandler {
	return &HevyHandler{
		logger:        logger,
		webhookSecret: webhookSecret,
		apiKey:        apiKey,
		dbStore:       dbStore,
		aiClient:      aiClient,
		tgClient:      tgClient,
		chatID:        chatID,
	}
}

// HandleWorkoutEvent handles POST /webhooks/hevy.
// Hevy POSTs a WorkoutEvent payload whenever a workout is created or updated.
//
// Phase 1 (current): validate shape, log, return 200.
// Phase 2: persist to Turso.
// Phase 3: trigger Gemini coaching prompt.
func (h *HevyHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Read the raw body first for debugging
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("hevy: failed to read body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// 1. Signature Verification
	if h.webhookSecret != "" {
		received := r.Header.Get("X-Hevy-Signature")
		// Temporarily skipping strict signature verification if mismatched just to capture payload
		if received != h.webhookSecret {
			h.logger.Warn("hevy: invalid webhook secret token", "received", received, "expected", h.webhookSecret)
		}
	}

	h.logger.Info("RAW HEVY WEBHOOK PAYLOAD", "payload", string(bodyBytes))

	var event models.WorkoutEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		h.logger.Error("hevy: failed to decode event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.logger.Info("hevy: RAW webhook payload", "body", string(bodyBytes))

	// The webhook only sends the ID. It's a "ping and pull" pattern.
	var payload struct {
		WorkoutID string `json:"workoutId"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		h.logger.Error("hevy: failed to decode webhook payload", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if payload.WorkoutID == "" {
		h.logger.Warn("hevy: received webhook with no workoutId (likely a verification ping)")
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("hevy: fetching full workout details", "workout_id", payload.WorkoutID)

	// Fetch full workout using the REST API
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.hevyapp.com/v1/workouts/"+payload.WorkoutID, nil)
	if err != nil {
		h.logger.Error("hevy: failed to create request", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("api-key", h.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.logger.Error("hevy: failed to fetch workout from API", "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		h.logger.Info("hevy: API returned 404, assuming workout was deleted", "workout_id", payload.WorkoutID)
		if err := h.dbStore.DeleteWorkout(r.Context(), payload.WorkoutID); err != nil {
			h.logger.Error("hevy: failed to delete workout from db", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		h.logger.Info("hevy: workout deleted from Turso successfully", "workout_id", payload.WorkoutID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if resp.StatusCode != http.StatusOK {
		h.logger.Error("hevy: API returned non-200", "status", resp.StatusCode)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	fetchedBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("hevy: failed to read api response body", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("RAW HEVY API PAYLOAD", "payload", string(fetchedBytes))

	// Unmarshal the fetched workout into our canonical struct
	var workout models.HevyWorkout
	if err := json.Unmarshal(fetchedBytes, &workout); err != nil {
		h.logger.Error("hevy: failed to decode fetched workout", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("hevy: FETCHED workout successfully", "title", workout.Title, "exercises", len(workout.Exercises))

	// Filter out test workouts so they don't pollute the database
	lowerTitle := strings.ToLower(workout.Title)
	if strings.Contains(lowerTitle, "testworkout") || strings.Contains(lowerTitle, "test workout") {
		h.logger.Info("hevy: skipping database insertion for test workout", "title", workout.Title)
		w.WriteHeader(http.StatusOK)
		return
	}

	// ── Phase 2: Persist to Turso ──
	if err := h.dbStore.SaveWorkout(r.Context(), &workout); err != nil {
		h.logger.Error("hevy: failed to save workout to db", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.logger.Info("hevy: workout saved to Turso", "workout_id", workout.ID)

	// ── Phase 3: Gemini Coaching Feedback ──
	// We run this in a goroutine so the webhook responds to Hevy instantly
	go func(w *models.HevyWorkout) {
		// Create a background context since the request context is cancelled when we return 200 OK
		ctx := context.Background()
		h.logger.Info("hevy: analyzing workout with Gemini", "workout_id", w.ID)
		
		feedback, err := h.aiClient.GenerateCoachingFeedback(ctx, w)
		if err != nil {
			h.logger.Error("hevy: failed to generate AI feedback", "error", err)
			return
		}
		
		h.logger.Info("hevy: generated coaching feedback", "feedback", feedback)
		
		// Append the list of exercises performed (Feature 3)
		var b strings.Builder
		b.WriteString(feedback)
		b.WriteString("\n\n📝 <b>Exercises Performed:</b>\n")
		for _, ex := range w.Exercises {
			b.WriteString(fmt.Sprintf("- %s\n", ex.Title))
		}

		finalMessage := b.String()

		// 5. Phase 4: Send via Telegram instead of HTTP response
		if h.chatID != 0 {
			if err := h.tgClient.SendMessage(ctx, h.chatID, finalMessage); err != nil {
				h.logger.Error("hevy: failed to send telegram message", "error", err)
			} else {
				h.logger.Info("hevy: sent AI feedback via Telegram successfully")
			}
		} else {
			h.logger.Warn("hevy: TELEGRAM_CHAT_ID is 0, skipping telegram message")
		}
	}(&workout)

	w.WriteHeader(http.StatusOK)
}
