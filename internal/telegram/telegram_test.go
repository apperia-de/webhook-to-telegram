package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendMessage(t *testing.T) {
	threadID := int64(454)
	tests := []struct {
		name        string
		chatID      int64
		text        string
		opts        *SendMessageOptions
		mockStatus  int
		mockBody    string
		expectError bool
	}{
		{
			name:        "Success with HTML",
			chatID:      12345,
			text:        "Hello HTML",
			opts:        &SendMessageOptions{ParseMode: HTML},
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success with DisableLinkPreview",
			chatID:      12345,
			text:        "Hello NoPreview",
			opts:        &SendMessageOptions{ParseMode: HTML, DisableLinkPreview: true},
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success with MessageThreadID",
			chatID:      12345,
			text:        "Hello Topic",
			opts:        &SendMessageOptions{ParseMode: HTML, MessageThreadID: &threadID},
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success with DisableNotification and ProtectContent",
			chatID:      12345,
			text:        "Hello Silent Protected",
			opts:        &SendMessageOptions{DisableNotification: true, ProtectContent: true},
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success without options (nil)",
			chatID:      12345,
			text:        "Hello Plain",
			opts:        nil,
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success with MarkdownV2",
			chatID:      12345,
			text:        "Hello MarkdownV2",
			opts:        &SendMessageOptions{ParseMode: MarkdownV2},
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "API Error Response",
			chatID:      12345,
			text:        "Hello Bad",
			opts:        nil,
			mockStatus:  http.StatusBadRequest,
			mockBody:    `{"ok": false, "description": "Bad Request"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := CustomClient("http://mock-telegram", "fake-token")

			client.hc.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://mock-telegram/sendMessage" {
					return nil, fmt.Errorf("unexpected URL: %s", req.URL)
				}
				if req.Method != http.MethodPost {
					return nil, fmt.Errorf("unexpected method: %s", req.Method)
				}
				if req.Header.Get("Content-Type") != "application/json" {
					return nil, fmt.Errorf("unexpected Content-Type: %s", req.Header.Get("Content-Type"))
				}

				// Verify JSON payload
				var payload map[string]any
				err := json.NewDecoder(req.Body).Decode(&payload)
				if err != nil {
					return nil, err
				}

				if int64(payload["chat_id"].(float64)) != tt.chatID {
					return nil, fmt.Errorf("unexpected chat_id: %v", payload["chat_id"])
				}
				if payload["text"].(string) != tt.text {
					return nil, fmt.Errorf("unexpected text: %s", payload["text"])
				}

				if tt.opts != nil && tt.opts.DisableLinkPreview {
					opts, ok := payload["link_preview_options"].(map[string]any)
					if !ok || opts["is_disabled"] != true {
						return nil, fmt.Errorf("unexpected link_preview_options: %v", payload["link_preview_options"])
					}
				} else {
					if _, ok := payload["link_preview_options"]; ok {
						return nil, fmt.Errorf("link_preview_options should not be set")
					}
				}

				if tt.opts != nil && tt.opts.DisableNotification {
					if disabled, ok := payload["disable_notification"].(bool); !ok || !disabled {
						return nil, fmt.Errorf("unexpected disable_notification: %v", payload["disable_notification"])
					}
				} else {
					if _, ok := payload["disable_notification"]; ok {
						return nil, fmt.Errorf("disable_notification should not be set")
					}
				}

				if tt.opts != nil && tt.opts.ProtectContent {
					if protect, ok := payload["protect_content"].(bool); !ok || !protect {
						return nil, fmt.Errorf("unexpected protect_content: %v", payload["protect_content"])
					}
				} else {
					if _, ok := payload["protect_content"]; ok {
						return nil, fmt.Errorf("protect_content should not be set")
					}
				}

				if tt.opts != nil && tt.opts.MessageThreadID != nil {
					if int64(payload["message_thread_id"].(float64)) != *tt.opts.MessageThreadID {
						return nil, fmt.Errorf("unexpected message_thread_id: %v", payload["message_thread_id"])
					}
				} else {
					if _, ok := payload["message_thread_id"]; ok {
						return nil, fmt.Errorf("message_thread_id should not be set")
					}
				}

				if tt.opts != nil && tt.opts.ParseMode != "" {
					if payload["parse_mode"].(string) != string(tt.opts.ParseMode) {
						return nil, fmt.Errorf("unexpected parse_mode: %v", payload["parse_mode"])
					}
				} else {
					if _, ok := payload["parse_mode"]; ok {
						return nil, fmt.Errorf("parse_mode should not be set")
					}
				}

				resp := &http.Response{
					StatusCode: tt.mockStatus,
					Body:       io.NopCloser(bytes.NewBufferString(tt.mockBody)),
					Header:     make(http.Header),
				}
				return resp, nil
			})

			err := client.SendMessage(context.Background(), tt.chatID, tt.text, tt.opts)
			if (err != nil) != tt.expectError {
				t.Errorf("SendMessage() error = %v, expectError = %v", err, tt.expectError)
			}
		})
	}
}

func TestSendMessageContextCanceled(t *testing.T) {
	client := CustomClient("http://mock-telegram", "fake-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.SendMessage(ctx, 12345, "Hello", nil)
	if err == nil {
		t.Error("expected error due to canceled context, got nil")
	}
}

func TestSendMessageRetriesOn429(t *testing.T) {
	client := CustomClient("http://mock-telegram", "fake-token")

	var requests int
	client.hc.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "error_code": 429, "description": "Too Many Requests: retry after 1", "parameters": {"retry_after": 1}}`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok": true}`)),
			Header:     make(http.Header),
		}, nil
	})

	if err := client.SendMessage(context.Background(), 12345, "Hello Retry", nil); err != nil {
		t.Errorf("SendMessage() should succeed after retry, got error: %v", err)
	}
	if requests != 2 {
		t.Errorf("expected 2 requests (429 then 200), got %d", requests)
	}
}

func TestSendMessageRateLimitContextCanceled(t *testing.T) {
	client := CustomClient("http://mock-telegram", "fake-token")

	client.hc.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "error_code": 429, "description": "Too Many Requests: retry after 10", "parameters": {"retry_after": 10}}`)),
			Header:     make(http.Header),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.SendMessage(ctx, 12345, "Hello RateLimitCancel", nil)
	if err == nil {
		t.Error("expected error due to context timeout during rate limit sleep, got nil")
	}
}

func TestRetryAfter(t *testing.T) {
	if got := retryAfter([]byte(`{"parameters": {"retry_after": 5}}`)); got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}
	if got := retryAfter([]byte(`not-json`)); got != time.Second {
		t.Errorf("expected 1s fallback, got %v", got)
	}
	if got := retryAfter([]byte(`{"ok": false}`)); got != time.Second {
		t.Errorf("expected 1s fallback, got %v", got)
	}
}

func TestSetWebhook(t *testing.T) {
	tests := []struct {
		name        string
		webhookURL  string
		mockStatus  int
		mockBody    string
		expectError bool
	}{
		{
			name:        "Success",
			webhookURL:  "https://example.com/bot-webhook",
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "API Error",
			webhookURL:  "https://example.com/bot-webhook",
			mockStatus:  http.StatusForbidden,
			mockBody:    `{"ok": false, "description": "Forbidden"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := CustomClient("http://mock-telegram", "fake-token")

			client.hc.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://mock-telegram/setWebhook" {
					return nil, fmt.Errorf("unexpected URL: %s", req.URL)
				}
				if req.Method != http.MethodPost {
					return nil, fmt.Errorf("unexpected method: %s", req.Method)
				}

				// Verify JSON payload
				var payload map[string]any
				err := json.NewDecoder(req.Body).Decode(&payload)
				if err != nil {
					return nil, err
				}

				if payload["url"].(string) != tt.webhookURL {
					return nil, fmt.Errorf("unexpected url: %s", payload["url"])
				}

				resp := &http.Response{
					StatusCode: tt.mockStatus,
					Body:       io.NopCloser(bytes.NewBufferString(tt.mockBody)),
					Header:     make(http.Header),
				}
				return resp, nil
			})

			err := client.SetWebhook(context.Background(), tt.webhookURL)
			if (err != nil) != tt.expectError {
				t.Errorf("SetWebhook() error = %v, expectError = %v", err, tt.expectError)
			}
		})
	}
}

func TestSetWebhookContextCanceled(t *testing.T) {
	client := CustomClient("http://mock-telegram", "fake-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.SetWebhook(ctx, "https://example.com/bot")
	if err == nil {
		t.Error("expected error due to canceled context, got nil")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("some-token")
	if client.botToken != "some-token" {
		t.Errorf("expected botToken to be some-token, got %s", client.botToken)
	}
	expectedURL := "https://api.telegram.org/botsome-token"
	if client.apiURL != expectedURL {
		t.Errorf("expected apiURL to be %s, got %s", expectedURL, client.apiURL)
	}
}
