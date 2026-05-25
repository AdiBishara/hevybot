package db

import (
	"context"
	"database/sql"
	"fmt"
)

var schemaQueries = []string{
	`CREATE TABLE IF NOT EXISTS workouts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		routine_id TEXT,
		start_time DATETIME NOT NULL,
		end_time DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS exercises (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workout_id TEXT NOT NULL,
		idx INTEGER NOT NULL,
		title TEXT NOT NULL,
		notes TEXT,
		exercise_template_id TEXT NOT NULL,
		superset_id INTEGER,
		FOREIGN KEY(workout_id) REFERENCES workouts(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE IF NOT EXISTS sets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exercise_id INTEGER NOT NULL,
		idx INTEGER NOT NULL,
		set_type TEXT NOT NULL,
		weight_kg REAL,
		reps INTEGER,
		distance_meters REAL,
		duration_seconds INTEGER,
		rpe REAL,
		FOREIGN KEY(exercise_id) REFERENCES exercises(id) ON DELETE CASCADE
	);`,
}

// RunMigrations connects to Turso and creates the tables if they don't exist.
func RunMigrations(ctx context.Context, dbURL, authToken string) error {
	url := fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	db, err := sql.Open("libsql", url)
	if err != nil {
		return err
	}
	defer db.Close()

	for _, query := range schemaQueries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute migration: %w", err)
		}
	}
	return nil
}
