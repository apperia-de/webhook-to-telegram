package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sknr/webhook-to-telegram/internal/telegram"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetValue(t *testing.T) {
	msg := map[string]any{
		"action": "started",
		"repository": map[string]any{
			"name":             "my-repo",
			"stargazers_count": float64(100),
			"owner": map[string]any{
				"login": "octocat",
			},
		},
		"null_val": nil,
		"int_val":  42,
	}

	header := make(http.Header)
	header.Set("X-Test-Header", "header-value")

	tests := []struct {
		name     string
		key      string
		expected any
	}{
		{"Simple key", "action", "started"},
		{"Nested key depth 2", "repository.name", "my-repo"},
		{"Nested key depth 3", "repository.owner.login", "octocat"},
		{"Number key", "repository.stargazers_count", float64(100)},
		{"Header key", "header:X-Test-Header", "header-value"},
		{"Missing simple key", "non_existent", nil},
		{"Missing nested key", "repository.non_existent", nil},
		{"Missing deep nested key", "repository.owner.non_existent", nil},
		{"Invalid intermediate type", "repository.name.non_existent", nil},
		{"Null value key", "null_val", nil},
		{"Header split mismatch", "header:too:many:colons", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getValue(tt.key, msg, header)
			if got != tt.expected {
				t.Errorf("getValue(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestEscapeText(t *testing.T) {
	s := &WebhookServer{}

	tests := []struct {
		name      string
		parseMode telegram.ParseMode
		input     string
		expected  string
	}{
		{"HTML simple", telegram.HTML, "hello <world> & friends", "hello &lt;world&gt; &amp; friends"},
		{"Markdown simple", telegram.Markdown, "hello _world_ *bold* `code` [link", "hello \\_world\\_ \\*bold\\* \\`code\\` \\[link"},
		{"MarkdownV2 all", telegram.MarkdownV2, "_*[]()~`>#+-=|{}.!", "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!"},
		{"Invalid/None parse mode", "none", "hello", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.escapeText(tt.parseMode, tt.input)
			if got != tt.expected {
				t.Errorf("escapeText(%q, %q) = %q, want %q", tt.parseMode, tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	s := &WebhookServer{}
	msg := map[string]any{
		"token": "secret-token",
	}
	req := httptest.NewRequest("POST", "/webhooks/test", nil)
	req.Header.Set("X-Hook-ID", "12345")

	tests := []struct {
		name     string
		webhook  *Webhook
		expected bool
	}{
		{
			name: "TypeNone",
			webhook: &Webhook{
				Verification: ValidationType{Type: TypeNone},
			},
			expected: true,
		},
		{
			name: "TypeHeader Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHeader,
					Key:   "X-Hook-ID",
					Value: "12345",
				},
			},
			expected: true,
		},
		{
			name: "TypeHeader Mismatch",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHeader,
					Key:   "X-Hook-ID",
					Value: "wrong",
				},
			},
			expected: false,
		},
		{
			name: "TypeMessage Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeMessage,
					Key:   "token",
					Value: "secret-token",
				},
			},
			expected: true,
		},
		{
			name: "TypeMessage Mismatch",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeMessage,
					Key:   "token",
					Value: "wrong-token",
				},
			},
			expected: false,
		},
		{
			name: "Unknown verification type",
			webhook: &Webhook{
				Verification: ValidationType{
					Type: "invalid",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.isValid(tt.webhook, req, msg)
			if got != tt.expected {
				t.Errorf("isValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFindMatchingTemplate(t *testing.T) {
	msg := map[string]any{
		"action": "created",
		"status": "success",
	}
	req := httptest.NewRequest("POST", "/webhooks/test", nil)
	req.Header.Set("X-GitHub-Event", "star")

	t1 := &Template{
		Template: "T1",
		Trigger: &TriggerType{
			Type:  TypeHeader,
			Key:   "X-GitHub-Event",
			Value: "star",
		},
	}
	t2 := &Template{
		Template: "T2",
		Trigger: &TriggerType{
			Type:  TypeMessage,
			Key:   "action",
			Value: "created",
		},
	}
	t3 := &Template{
		Template: "Default",
	}

	templates := []*Template{t1, t2, t3}

	got, err := findMatchingTemplate(req, msg, templates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != t1 {
		t.Errorf("expected t1, got %v", got)
	}

	req2 := httptest.NewRequest("POST", "/webhooks/test", nil)
	got, err = findMatchingTemplate(req2, msg, templates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != t2 {
		t.Errorf("expected t2, got %v", got)
	}

	msg3 := map[string]any{"action": "deleted"}
	got, err = findMatchingTemplate(req2, msg3, templates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != t3 {
		t.Errorf("expected t3, got %v", got)
	}

	invalidTriggerTemplate := &Template{
		Trigger: &TriggerType{Type: "invalid-type"},
	}
	_, err = findMatchingTemplate(req2, msg3, []*Template{invalidTriggerTemplate})
	if err == nil {
		t.Error("expected error for invalid trigger type, got nil")
	}
}

func TestWebhookHandlerSuccess(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	var receivedTelegramRequest bool

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.telegram.org" && strings.Contains(req.URL.Path, "/botfake-token/sendMessage") {
			receivedTelegramRequest = true
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(`{"ok": true, "result": {"message_id": 12345}}`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}
		if req.URL.Host == "api.telegram.org" && strings.Contains(req.URL.Path, "/botfake-token/setWebhook") {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(`{"ok": true}`)),
			}
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}
		return nil, fmt.Errorf("unexpected request to %s", req.URL)
	})

	cfgContent := `
telegram:
  botToken: "fake-token"
  chatID: 987654
  webhookURL: "https://example.com"
webhooks:
  - name: test-webhook
    pattern: test-hook
    contentType: application/json
    parseMode: html
    verification:
      type: none
    templates:
      - template: "Event: <b>%s</b>"
        keys:
          - event
`
	err := os.WriteFile(ConfigFile, []byte(cfgContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	defer func() { _ = os.Remove(ConfigFile) }()

	s, err := New()
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	recorder := httptest.NewRecorder()
	payload := `{"event": "<test>"}`
	req := httptest.NewRequest("POST", "/webhooks/test-hook", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")

	s.mux.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", recorder.Code, recorder.Body.String())
	}

	if !receivedTelegramRequest {
		t.Error("expected Telegram API call, but none was received")
	}
}
