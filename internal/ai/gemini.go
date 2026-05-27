package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yourusername/hevybot/internal/models"
)

// Client defines the interface for our AI operations.
type Client interface {
	GenerateCoachingFeedback(ctx context.Context, w *models.HevyWorkout) (string, error)
	Chat(ctx context.Context, text string) (string, error)
	ClassifyIntent(ctx context.Context, text string) (string, error)
}

type geminiClient struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiClient returns a new AI client using the provided Google AI Studio key and model name.
func NewGeminiClient(apiKey, model string) Client {
	return &geminiClient{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second}, // Increased to 60s for massive routine generation
	}
}

// GenerateCoachingFeedback formats the workout and sends it to Gemini for analysis.
func (c *geminiClient) GenerateCoachingFeedback(ctx context.Context, w *models.HevyWorkout) (string, error) {
	prompt := buildPrompt(w)

	// Google AI Studio REST API format
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": "You are an elite, no-nonsense strength and conditioning coach. You review the user's workout and provide exactly 2-3 short, punchy bullet points of feedback. Focus on volume, intensity, exercise selection, or congratulations on PRs. Do not use hashtags or overly enthusiastic emojis. Keep it concise, brutal, and motivating."},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.7,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse the response
	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if len(res.Candidates) > 0 && len(res.Candidates[0].Content.Parts) > 0 {
		return res.Candidates[0].Content.Parts[0].Text, nil
	}

	return "No feedback generated.", nil
}

// buildPrompt converts the structured workout into a readable text format for the LLM.
func buildPrompt(w *models.HevyWorkout) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workout Title: %s\n", w.Title))
	sb.WriteString(fmt.Sprintf("Duration: %s to %s\n", w.StartTime, w.EndTime))
	sb.WriteString("Exercises:\n")

	for _, ex := range w.Exercises {
		sb.WriteString(fmt.Sprintf("- %s\n", ex.Title))
		for _, set := range ex.Sets {
			weight := 0.0
			if set.WeightKG != nil {
				weight = *set.WeightKG
			}
			reps := 0
			if set.Reps != nil {
				reps = *set.Reps
			}
			rpe := 0.0
			if set.RPE != nil {
				rpe = *set.RPE
			}
			
			// Handle Cardio / Duration-based sets
			if weight == 0 && reps == 0 && (set.DistanceMeters != nil || set.DurationSeconds != nil) {
				dist := 0.0
				if set.DistanceMeters != nil {
					dist = *set.DistanceMeters
				}
				dur := 0
				if set.DurationSeconds != nil {
					dur = *set.DurationSeconds
				}
				sb.WriteString(fmt.Sprintf("  Set %d: %.1f meters, %d seconds (RPE: %.1f)\n", set.Index+1, dist, dur, rpe))
			} else {
				sb.WriteString(fmt.Sprintf("  Set %d: %.1f kg x %d reps (RPE: %.1f)\n", set.Index+1, weight, reps, rpe))
			}
		}
	}
	return sb.String()
}

// Chat sends a raw text prompt to Gemini.
func (c *geminiClient) Chat(ctx context.Context, text string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": text},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if len(res.Candidates) > 0 && len(res.Candidates[0].Content.Parts) > 0 {
		return res.Candidates[0].Content.Parts[0].Text, nil
	}

	return "No response from AI.", nil
}

// ClassifyIntent sends a strict prompt to Gemini to classify the user's intent.
func (c *geminiClient) ClassifyIntent(ctx context.Context, text string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)

	prompt := `You are an intent classification engine. Classify the user's message into EXACTLY ONE of the following buckets. Return ONLY the bucket name, nothing else. No markdown, no quotes.

Buckets:
ROUTINE_GEN: Asking for a new workout plan or split.
AUDIBLE: Asking to swap an exercise or tweak an active workout.
ANALYTICS: Asking about their progress, strength, stats, or weakest muscles.
FORM_CHECK: Asking how to perform an exercise.
NUTRITION: Asking about diet, macros, or supplements.
MOTIVATION: Asking to be hyped up or motivated.
GENERAL: General fitness Q&A or anything else.

User Message: ` + text

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.0, // strict deterministic output
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "GENERAL", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "GENERAL", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "GENERAL", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "GENERAL", fmt.Errorf("gemini API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "GENERAL", err
	}

	if len(res.Candidates) > 0 && len(res.Candidates[0].Content.Parts) > 0 {
		return strings.TrimSpace(res.Candidates[0].Content.Parts[0].Text), nil
	}

	return "GENERAL", nil
}
