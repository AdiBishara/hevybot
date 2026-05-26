package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yourusername/hevybot/internal/models"
)

// Client defines the interface for our Telegram operations.
type Client interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendKeyboard(ctx context.Context, chatID int64, text string, keyboard interface{}) error
	AnswerCallbackQuery(ctx context.Context, callbackQueryID string) error
	SetMyCommands(ctx context.Context, commands []models.BotCommand) error
}

type tgClient struct {
	botToken string
	client   *http.Client
}

// NewTelegramClient returns a new Telegram client.
func NewTelegramClient(botToken string) Client {
	return &tgClient{
		botToken: botToken,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// SendMessage sends a text message to a specific Telegram chat.
func (c *tgClient) SendMessage(ctx context.Context, chatID int64, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)

	reqBody := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// SendKeyboard sends a text message with an inline keyboard to a specific Telegram chat.
func (c *tgClient) SendKeyboard(ctx context.Context, chatID int64, text string, keyboard interface{}) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)

	reqBody := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// AnswerCallbackQuery sends a request to Telegram to stop the loading spinner on an inline button.
func (c *tgClient) AnswerCallbackQuery(ctx context.Context, callbackQueryID string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", c.botToken)

	reqBody := map[string]interface{}{
		"callback_query_id": callbackQueryID,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram answer callback returned status %d: %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// SetMyCommands sets the list of the bot's commands for the Telegram menu.
func (c *tgClient) SetMyCommands(ctx context.Context, commands []models.BotCommand) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", c.botToken)

	reqBody := map[string]interface{}{
		"commands": commands,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	return nil
}
