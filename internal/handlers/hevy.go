package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/yourusername/hevybot/internal/db"
	"github.com/yourusername/hevybot/internal/models"
)

// HevyHandler groups all dependencies needed by Hevy webhook handlers.
// Unexported fields are injected via NewHevyHandler — no globals.
type HevyHandler struct {
	logger        *slog.Logger
	webhookSecret string
	apiKey        string
	dbStore       db.Store
	// ai    ai.Client     ← injected in Phase 3
}

// NewHevyHandler constructs a HevyHandler with its dependencies.
func NewHevyHandler(logger *slog.Logger, webhookSecret, apiKey string, dbStore db.Store) *HevyHandler {
	return &HevyHandler{
		logger:        logger,
		webhookSecret: webhookSecret,
		apiKey:        apiKey,
		dbStore:       dbStore,
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

	// Read raw body first
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("hevy: failed to read body", "error", err)
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

	// Unmarshal the fetched workout into our canonical struct
	var workout models.HevyWorkout
	if err := json.Unmarshal(fetchedBytes, &workout); err != nil {
		h.logger.Error("hevy: failed to decode fetched workout", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("hevy: FETCHED workout successfully", "title", workout.Title, "exercises", len(workout.Exercises))

	// ── Phase 2: Persist to Turso ──
	if err := h.dbStore.SaveWorkout(r.Context(), &workout); err != nil {
		h.logger.Error("hevy: failed to save workout to db", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.logger.Info("hevy: workout saved to Turso", "workout_id", workout.ID)

	// ── Phase 3: ai.GenerateCoachingFeedback(r.Context(), workout) ──

	w.WriteHeader(http.StatusOK)
}
