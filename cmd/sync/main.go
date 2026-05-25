package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/yourusername/hevybot/internal/config"
	"github.com/yourusername/hevybot/internal/db"
	"github.com/yourusername/hevybot/internal/models"
)

type paginatedResponse struct {
	Workouts []models.HevyWorkout `json:"workouts"`
	Page     int                  `json:"page"`
	PageCount int                 `json:"page_count"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	logger.Info("starting historical data sync...")

	// 1. Load configuration (uses Koyeb env vars)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Connect to Database
	store, err := db.NewTursoStore(cfg.TursoDBURL, cfg.TursoAuthToken)
	if err != nil {
		logger.Error("failed to connect to turso", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to turso database successfully")

	ctx := context.Background()
	client := &http.Client{Timeout: 10 * time.Second}
	page := 1
	totalSynced := 0

	// 3. Paginate through Hevy API
	for {
		logger.Info("fetching page from hevy", "page", page)

		url := fmt.Sprintf("https://api.hevyapp.com/v1/workouts?page=%d&pageSize=10", page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			logger.Error("failed to create request", "error", err)
			os.Exit(1)
		}

		req.Header.Set("api-key", cfg.HevyAPIKey)
		req.Header.Set("accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			logger.Error("hevy api request failed", "error", err)
			os.Exit(1)
		}

		if resp.StatusCode != http.StatusOK {
			logger.Error("hevy api returned non-200", "status", resp.StatusCode)
			resp.Body.Close()
			os.Exit(1)
		}

		var payload paginatedResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			logger.Error("failed to decode response", "error", err)
			resp.Body.Close()
			os.Exit(1)
		}
		resp.Body.Close()

		if len(payload.Workouts) == 0 {
			logger.Info("no more workouts found. sync complete!")
			break
		}

		// Save each workout
		for _, w := range payload.Workouts {
			// SaveWorkout expects a pointer
			if err := store.SaveWorkout(ctx, &w); err != nil {
				logger.Error("failed to save workout to db", "workout_id", w.ID, "error", err)
				continue
			}
			totalSynced++
		}

		logger.Info("page complete", "page", page, "synced_so_far", totalSynced)

		// Respect rate limits, wait 1 second between pages
		time.Sleep(1 * time.Second)
		page++
	}

	logger.Info("sync finished successfully", "total_workouts_synced", totalSynced)
}
