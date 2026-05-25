package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"github.com/yourusername/hevybot/internal/models"
)

// Store defines the interface for database operations.
type Store interface {
	SaveWorkout(ctx context.Context, w *models.HevyWorkout) error
}

type tursoStore struct {
	db *sql.DB
}

// NewTursoStore connects to the Turso database and returns a Store implementation.
func NewTursoStore(dbURL, authToken string) (Store, error) {
	url := fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	db, err := sql.Open("libsql", url)
	if err != nil {
		return nil, err
	}
	
	// Ping ensures the connection parameters are correct right away.
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	
	return &tursoStore{db: db}, nil
}

// SaveWorkout persists a full Hevy workout payload.
func (s *tursoStore) SaveWorkout(ctx context.Context, w *models.HevyWorkout) error {
	// Use a transaction to ensure all or nothing is saved
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Upsert the Workout (ON CONFLICT REPLACE in case it was edited)
	workoutQuery := `
		INSERT INTO workouts (id, title, routine_id, start_time, end_time, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title,
			routine_id=excluded.routine_id,
			start_time=excluded.start_time,
			end_time=excluded.end_time,
			updated_at=excluded.updated_at
	`
	_, err = tx.ExecContext(ctx, workoutQuery,
		w.ID, w.Title, w.RoutineID, w.StartTime, w.EndTime, w.UpdatedAt, w.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert workout: %w", err)
	}

	// 2. Delete existing exercises/sets for this workout (simplest way to handle an update)
	if _, err := tx.ExecContext(ctx, "DELETE FROM exercises WHERE workout_id = ?", w.ID); err != nil {
		return fmt.Errorf("delete old exercises: %w", err)
	}

	// 3. Insert Exercises & Sets
	for _, ex := range w.Exercises {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO exercises (workout_id, idx, title, notes, exercise_template_id, superset_id)
			VALUES (?, ?, ?, ?, ?, ?)
		`, w.ID, ex.Index, ex.Title, ex.Notes, ex.ExerciseTemplateID, ex.SupersetID)
		if err != nil {
			return fmt.Errorf("insert exercise: %w", err)
		}

		exID, _ := res.LastInsertId()

		for _, set := range ex.Sets {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO sets (exercise_id, idx, set_type, weight_kg, reps, distance_meters, duration_seconds, rpe)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, exID, set.Index, set.Type, set.WeightKG, set.Reps, set.DistanceMeters, set.DurationSeconds, set.RPE)
			if err != nil {
				return fmt.Errorf("insert set: %w", err)
			}
		}
	}

	return tx.Commit()
}
