package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/yourusername/hevybot/internal/models"
)

// HevyHandler groups all dependencies needed by Hevy webhook handlers.
// Unexported fields are injected via NewHevyHandler — no globals.
type HevyHandler struct {
	logger        *slog.Logger
	webhookSecret string
	// db    db.Store      ← injected in Phase 2
	// ai    ai.Client     ← injected in Phase 3
}

// NewHevyHandler constructs a HevyHandler with its dependencies.
func NewHevyHandler(logger *slog.Logger, webhookSecret string) *HevyHandler {
	return &HevyHandler{
		logger:        logger,
		webhookSecret: webhookSecret,
	}
}

// HandleWorkoutEvent handles POST /webhooks/hevy.
// Hevy POSTs a WorkoutEvent payload whenever a workout is created or updated.
//
// Phase 1 (current): validate shape, log, return 200.
// Phase 2: persist to Turso.
// Phase 3: trigger Gemini coaching prompt.
func (h *HevyHandler) HandleWorkoutEvent(w http.ResponseWriter, r *http.Request) {
	// ── Signature validation (TODO: implement HMAC check in Phase 1 hardening) ──
	// secret := r.Header.Get("X-Hevy-Signature")
	// if !validateHMACSignature(secret, h.webhookSecret, body) { ... }

	// Read raw body first to see exactly what Hevy is sending
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("hevy: failed to read body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// LOG THE EXACT RAW PAYLOAD
	h.logger.Info("hevy: RAW payload", "body", string(bodyBytes))

	var event models.WorkoutEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		h.logger.Error("hevy: failed to decode workout event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if event.Workout == nil {
		h.logger.Warn("hevy: received event with nil workout payload", "type", event.Type)
		// Acknowledge receipt — Hevy will retry on non-2xx
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("hevy: received workout event",
		"event_type", event.Type,
		"workout_id", event.Workout.ID,
		"workout_title", event.Workout.Title,
		"exercise_count", len(event.Workout.Exercises),
	)

	// ── Phase 2: db.SaveWorkout(r.Context(), event.Workout) ──
	// ── Phase 3: ai.GenerateCoachingFeedback(r.Context(), event.Workout) ──

	w.WriteHeader(http.StatusOK)
}
