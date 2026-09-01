package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxSendAttempts limits how often a rate limited (HTTP 429) sendMessage
// call is retried before giving up.
const maxSendAttempts = 5

type ParseMode string

const (
	HTML       ParseMode = "HTML"
	Markdown   ParseMode = "Markdown"
	MarkdownV2 ParseMode = "MarkdownV2"
)

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

// SendMessageOptions contains optional parameters for sending a message.
type SendMessageOptions struct {
	ParseMode           ParseMode
	MessageThreadID     *int64
	DisableLinkPreview  bool
	DisableNotification bool
	ProtectContent      bool
}

type Client struct {
	botToken string
	apiURL   string
	hc       *http.Client
}

func NewClient(botToken string) *Client {
	return &Client{
		botToken: botToken,
		apiURL:   fmt.Sprintf("https://api.telegram.org/bot%s", botToken),
		hc:       &http.Client{Timeout: 10 * time.Second},
	}
}

// CustomClient allows using a mock or custom Telegram base URL (useful for testing)
func CustomClient(baseURL, botToken string) *Client {
	return &Client{
		botToken: botToken,
		apiURL:   baseURL,
		hc:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) SendMessage(chatID int64, text string, opts *SendMessageOptions) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if opts != nil {
		if opts.MessageThreadID != nil {
			payload["message_thread_id"] = *opts.MessageThreadID
		}
		if opts.ParseMode != "" {
			payload["parse_mode"] = string(opts.ParseMode)
		}
		if opts.DisableLinkPreview {
			payload["link_preview_options"] = map[string]any{"is_disabled": true}
		}
		if opts.DisableNotification {
			payload["disable_notification"] = true
		}
		if opts.ProtectContent {
			payload["protect_content"] = true
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.hc.Post(c.apiURL+"/sendMessage", "application/json", bytes.NewReader(data))
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		// Rate limited: wait for the duration Telegram tells us and try again.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxSendAttempts {
			time.Sleep(retryAfter(body))
			continue
		}

		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(body))
	}
}

// retryAfter extracts the retry_after duration from a 429 response body,
// falling back to one second when it cannot be parsed.
func retryAfter(body []byte) time.Duration {
	var apiErr struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Parameters.RetryAfter > 0 {
		return time.Duration(apiErr.Parameters.RetryAfter) * time.Second
	}
	return time.Second
}

func (c *Client) SetWebhook(webhookURL string) error {
	payload := map[string]any{
		"url": webhookURL,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.hc.Post(c.apiURL+"/setWebhook", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
