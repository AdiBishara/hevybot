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
	DeleteWorkout(ctx context.Context, workoutID string) error
	GetLastWorkoutDetailed(ctx context.Context) (*models.LastWorkoutStats, error)
	GetRecentWorkoutsDetailed(ctx context.Context, limit int) ([]models.LastWorkoutStats, error)
	GetStats(ctx context.Context) (totalWorkouts int, totalWeightKG float64, err error)
	GetMuscleGroup1RM(ctx context.Context, muscle string) ([]models.Exercise1RM, error)
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

// DeleteWorkout removes a workout and its associated exercises and sets.
// We manually cascade the deletes to ensure no orphan records remain, as SQLite
// foreign key pragmas might not be globally enabled.
func (s *tursoStore) DeleteWorkout(ctx context.Context, workoutID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete sets belonging to the exercises of this workout
	if _, err := tx.ExecContext(ctx, "DELETE FROM sets WHERE exercise_id IN (SELECT id FROM exercises WHERE workout_id = ?)", workoutID); err != nil {
		return fmt.Errorf("delete sets: %w", err)
	}

	// 2. Delete exercises
	if _, err := tx.ExecContext(ctx, "DELETE FROM exercises WHERE workout_id = ?", workoutID); err != nil {
		return fmt.Errorf("delete exercises: %w", err)
	}

	// 3. Delete the workout itself
	if _, err := tx.ExecContext(ctx, "DELETE FROM workouts WHERE id = ?", workoutID); err != nil {
		return fmt.Errorf("delete workout: %w", err)
	}

	return tx.Commit()
}

// GetLastWorkoutDetailed fetches the most recent workout title, time, total volume, total sets, and exercise list.
func (s *tursoStore) GetLastWorkoutDetailed(ctx context.Context) (*models.LastWorkoutStats, error) {
	var id, title, startTime string
	err := s.db.QueryRowContext(ctx, "SELECT id, title, start_time FROM workouts ORDER BY start_time DESC LIMIT 1").Scan(&id, &title, &startTime)
	if err == sql.ErrNoRows {
		return nil, nil // No workouts yet
	}
	if err != nil {
		return nil, err
	}

	stats := &models.LastWorkoutStats{
		Title:     title,
		StartTime: startTime,
	}

	// Get Volume and Sets
	var vol sql.NullFloat64
	var sets sql.NullInt64
	err = s.db.QueryRowContext(ctx, "SELECT SUM(s.weight_kg * s.reps), COUNT(s.id) FROM sets s JOIN exercises e ON s.exercise_id = e.id WHERE e.workout_id = ?", id).Scan(&vol, &sets)
	if err == nil {
		stats.Volume = vol.Float64
		stats.Sets = int(sets.Int64)
	}

	// Get Exercise List
	rows, err := s.db.QueryContext(ctx, "SELECT title FROM exercises WHERE workout_id = ? ORDER BY idx ASC", id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var exTitle string
			if err := rows.Scan(&exTitle); err == nil {
				stats.Exercises = append(stats.Exercises, exTitle)
			}
		}
	}

	return stats, nil
}

// GetRecentWorkoutsDetailed fetches a list of the most recent workouts with their exercises.
func (s *tursoStore) GetRecentWorkoutsDetailed(ctx context.Context, limit int) ([]models.LastWorkoutStats, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, title, start_time FROM workouts ORDER BY start_time DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workouts []models.LastWorkoutStats
	type wRow struct {
		id, title, startTime string
	}
	var rs []wRow
	for rows.Next() {
		var r wRow
		if err := rows.Scan(&r.id, &r.title, &r.startTime); err == nil {
			rs = append(rs, r)
		}
	}
	rows.Close() // explicitly close to free connection before running inner queries

	for _, r := range rs {
		stats := models.LastWorkoutStats{
			Title:     r.title,
			StartTime: r.startTime,
		}

		exRows, err := s.db.QueryContext(ctx, "SELECT title FROM exercises WHERE workout_id = ? ORDER BY idx ASC", r.id)
		if err == nil {
			for exRows.Next() {
				var exTitle string
				if err := exRows.Scan(&exTitle); err == nil {
					stats.Exercises = append(stats.Exercises, exTitle)
				}
			}
			exRows.Close()
		}
		workouts = append(workouts, stats)
	}

	return workouts, nil
}

// GetStats returns the total number of workouts and the total volume (weight) lifted across all time.
func (s *tursoStore) GetStats(ctx context.Context) (int, float64, error) {
	var totalWorkouts int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workouts").Scan(&totalWorkouts)
	if err != nil {
		return 0, 0, err
	}

	var totalWeight sql.NullFloat64
	err = s.db.QueryRowContext(ctx, "SELECT SUM(weight_kg * reps) FROM sets WHERE weight_kg IS NOT NULL AND reps IS NOT NULL").Scan(&totalWeight)
	if err != nil {
		return 0, 0, err
	}

	return totalWorkouts, totalWeight.Float64, nil
}

// GetMuscleGroup1RM calculates the max 1RM for each exercise in a muscle group using the Epley formula.
func (s *tursoStore) GetMuscleGroup1RM(ctx context.Context, muscle string) ([]models.Exercise1RM, error) {
	var likeClauses []string
	switch muscle {
	case "Chest":
		likeClauses = []string{"%bench%", "%fly%", "%push up%", "%push-up%", "%pec%"}
	case "Back":
		likeClauses = []string{"%pull up%", "%pull-up%", "%row%", "%lat%", "%deadlift%", "%shrug%"}
	case "Legs":
		likeClauses = []string{"%squat%", "%leg press%", "%extension%", "%curl%", "%calf%", "%lunge%"}
	case "Arms":
		likeClauses = []string{"%curl%", "%tricep%", "%extension%", "%pushdown%", "%skullcrusher%"}
	case "Shoulders":
		likeClauses = []string{"%overhead%", "%lateral%", "%front raise%", "%face pull%", "%delt%"}
	case "Core":
		likeClauses = []string{"%crunch%", "%plank%", "%sit up%", "%leg raise%", "%ab%"}
	default:
		return nil, fmt.Errorf("unknown muscle group: %s", muscle)
	}

	query := `SELECT e.title, 
	          MAX(CASE WHEN s.reps <= 15 THEN s.weight_kg * (1.0 + (s.reps / 30.0)) ELSE s.weight_kg END) as one_rm,
	          MAX(s.weight_kg) as max_weight 
	          FROM sets s JOIN exercises e ON s.exercise_id = e.id 
	          WHERE s.weight_kg IS NOT NULL AND s.reps IS NOT NULL AND (`
	args := []interface{}{}
	for i, clause := range likeClauses {
		if i > 0 {
			query += " OR "
		}
		query += "e.title LIKE ?"
		args = append(args, clause)
	}
	query += ") GROUP BY e.title ORDER BY one_rm DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.Exercise1RM
	for rows.Next() {
		var rm models.Exercise1RM
		var valRM sql.NullFloat64
		var valMax sql.NullFloat64
		if err := rows.Scan(&rm.Title, &valRM, &valMax); err != nil {
			return nil, err
		}
		rm.OneRM = valRM.Float64
		rm.MaxWeight = valMax.Float64
		results = append(results, rm)
	}

	return results, nil
}
