package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendMessage(t *testing.T) {
	threadID := int64(454)
	tests := []struct {
		name            string
		chatID          int64
		messageThreadID *int64
		text            string
		parseMode       ParseMode
		mockStatus      int
		mockBody        string
		expectError     bool
	}{
		{
			name:        "Success with HTML",
			chatID:      12345,
			text:        "Hello HTML",
			parseMode:   HTML,
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:            "Success with MessageThreadID",
			chatID:          12345,
			messageThreadID: &threadID,
			text:            "Hello Topic",
			parseMode:       HTML,
			mockStatus:      http.StatusOK,
			mockBody:        `{"ok": true}`,
			expectError:     false,
		},
		{
			name:        "Success without ParseMode",
			chatID:      12345,
			text:        "Hello Plain",
			parseMode:   "",
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "Success with MarkdownV2",
			chatID:      12345,
			text:        "Hello MarkdownV2",
			parseMode:   MarkdownV2,
			mockStatus:  http.StatusOK,
			mockBody:    `{"ok": true}`,
			expectError: false,
		},
		{
			name:        "API Error Response",
			chatID:      12345,
			text:        "Hello Bad",
			parseMode:   "",
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
				if tt.messageThreadID != nil {
					if int64(payload["message_thread_id"].(float64)) != *tt.messageThreadID {
						return nil, fmt.Errorf("unexpected message_thread_id: %v", payload["message_thread_id"])
					}
				} else {
					if _, ok := payload["message_thread_id"]; ok {
						return nil, fmt.Errorf("message_thread_id should not be set")
					}
				}
				if tt.parseMode != "" {
					if payload["parse_mode"].(string) != string(tt.parseMode) {
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

			err := client.SendMessage(tt.chatID, tt.messageThreadID, tt.text, tt.parseMode)
			if (err != nil) != tt.expectError {
				t.Errorf("SendMessage() error = %v, expectError = %v", err, tt.expectError)
			}
		})
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

			err := client.SetWebhook(tt.webhookURL)
			if (err != nil) != tt.expectError {
				t.Errorf("SetWebhook() error = %v, expectError = %v", err, tt.expectError)
			}
		})
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
