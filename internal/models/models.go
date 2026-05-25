package models

// HevyEventsResponse represents the top-level paginated payload from GET /v1/workouts/events.
type HevyEventsResponse struct {
	Page      int            `json:"page"`
	PageCount int            `json:"page_count"`
	Events    []WorkoutEvent `json:"events"`
}

// WorkoutEvent represents the wrapper around a single workout action.
type WorkoutEvent struct {
	Type    string       `json:"type"`    // e.g., "updated", "created"
	Workout *HevyWorkout `json:"workout"` // Pointer handles potential nulls if an event lacks full data
}

// HevyWorkout represents the core workout payload.
type HevyWorkout struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	RoutineID *string    `json:"routine_id"` // Pointer: can be null for ad-hoc workouts
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`
	UpdatedAt string     `json:"updated_at"`
	CreatedAt string     `json:"created_at"`
	Exercises []Exercise `json:"exercises"`
}

// Exercise represents an individual movement within a workout.
type Exercise struct {
	Index              int    `json:"index"`
	Title              string `json:"title"`
	Notes              string `json:"notes"`
	ExerciseTemplateID string `json:"exercise_template_id"`
	SupersetID         *int   `json:"superset_id"` // Pointer: null when not part of a superset
	Sets               []Set  `json:"sets"`
}

// Set represents the specific metrics captured for a single set.
type Set struct {
	Index           int      `json:"index"`
	Type            string   `json:"type"` // e.g., "normal", "warmup", "failure", "drop"
	WeightKG        *float64 `json:"weight_kg"`
	Reps            *int     `json:"reps"`
	DistanceMeters  *float64 `json:"distance_meters"`
	DurationSeconds *int     `json:"duration_seconds"`
	RPE             *float64 `json:"rpe"`
}

// TelegramUpdate represents the minimal structure of an inbound Telegram webhook payload.
// Extended in Phase 4 when command parsing is added.
type TelegramUpdate struct {
	UpdateID int             `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
}

// TelegramMessage holds the message body from a Telegram user.
type TelegramMessage struct {
	MessageID int           `json:"message_id"`
	From      *TelegramUser `json:"from,omitempty"`
	Chat      TelegramChat  `json:"chat"`
	Text      string        `json:"text,omitempty"`
}

// TelegramUser holds the sender identity.
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// TelegramChat identifies the chat context.
type TelegramChat struct {
	ID int64 `json:"id"`
}
