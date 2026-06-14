package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

func generateSWTToken(event string, body []byte, secret []byte) (string, error) {
	var bodyHash string
	if len(body) > 0 {
		h := sha256.Sum256(body)
		bodyHash = fmt.Sprintf("sha-256:%x", h[:])
	}

	type WebhookClaim struct {
		Event string `json:"event"`
		Hash  string `json:"hash,omitempty"`
	}
	type Claims struct {
		Webhook WebhookClaim `json:"webhook"`
		Exp     int64        `json:"exp"`
	}

	claims := &Claims{
		Webhook: WebhookClaim{
			Event: event,
			Hash:  bodyHash,
		},
		Exp: 9999999999, // far future
	}

	type Header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}

	header := Header{
		Alg: "HS256",
		Typ: "SWT",
	}

	headerBytes, _ := json.Marshal(header)
	headerSegment := base64.RawURLEncoding.EncodeToString(headerBytes)

	payloadBytes, _ := json.Marshal(claims)
	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(headerSegment + "." + payloadSegment))
	signatureSegment := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return headerSegment + "." + payloadSegment + "." + signatureSegment, nil
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

	req := httptest.NewRequest("POST", "/webhooks/test", nil)
	req.Header.Set("X-Test-Header", "header-value")

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
			got := getValue(tt.key, msg, req)
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

	// For non-SWT tests
	req := httptest.NewRequest("POST", "/webhooks/test", nil)
	req.Header.Set("X-Hook-ID", "12345")

	// For SWT test
	secretKey := []byte("my-swt-secret")
	bodyBytes := []byte(`{"customer":{"name":"alice"}}`)
	swtToken, err := generateSWTToken("payment.completed", bodyBytes, secretKey)
	if err != nil {
		t.Fatalf("failed to generate SWT: %v", err)
	}

	reqSWT := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(bodyBytes))
	reqSWT.Header.Set("Authorization", "Bearer "+swtToken)

	// For HMAC tests
	hmacSecret := "my-secret-key"
	hmacBody := []byte(`{"status":"ok"}`)

	// HMAC-SHA256 hex
	mac256 := hmac.New(sha256.New, []byte(hmacSecret))
	mac256.Write(hmacBody)
	sig256Hex := hex.EncodeToString(mac256.Sum(nil))

	reqHMAC256 := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMAC256.Header.Set("X-Hub-Signature-256", "sha256="+sig256Hex)

	reqHMAC256NoPrefix := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMAC256NoPrefix.Header.Set("X-Gitea-Signature", sig256Hex)

	// HMAC-SHA256 base64
	sig256Base64 := base64.StdEncoding.EncodeToString(mac256.Sum(nil))
	reqHMAC256Base64 := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMAC256Base64.Header.Set("X-Shopify-Hmac-SHA256", sig256Base64)

	// HMAC-SHA1 hex
	mac1 := hmac.New(sha1.New, []byte(hmacSecret))
	mac1.Write(hmacBody)
	sig1Hex := hex.EncodeToString(mac1.Sum(nil))
	reqHMAC1 := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMAC1.Header.Set("X-Hub-Signature", "sha1="+sig1Hex)

	// HMAC-SHA1 base64
	sig1Base64 := base64.StdEncoding.EncodeToString(mac1.Sum(nil))
	reqHMAC1Base64 := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMAC1Base64.Header.Set("X-Signature", sig1Base64)

	// Mismatched / Bad HMAC requests
	reqHMACBadSig := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))
	reqHMACBadSig.Header.Set("X-Hub-Signature-256", "sha256=9999999999999999999999999999999999999999999999999999999999999999")

	reqHMACMissingHeader := httptest.NewRequest("POST", "/webhooks/test", bytes.NewBuffer(hmacBody))

	tests := []struct {
		name      string
		webhook   *Webhook
		req       *http.Request
		bodyBytes []byte
		expected  bool
	}{
		{
			name: "TypeNone",
			webhook: &Webhook{
				Verification: ValidationType{Type: TypeNone},
			},
			req:      req,
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
			req:      req,
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
			req:      req,
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
			req:      req,
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
			req:      req,
			expected: false,
		},
		{
			name: "SWT Success Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  "swt",
					Value: string(secretKey),
				},
			},
			req:       reqSWT,
			bodyBytes: bodyBytes,
			expected:  true,
		},
		{
			name: "SWT Mismatch Token",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  "swt",
					Value: "wrong-secret-key",
				},
			},
			req:       reqSWT,
			bodyBytes: bodyBytes,
			expected:  false,
		},
		{
			name: "HMAC-SHA256 Hex Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256,
					Key:   "X-Hub-Signature-256",
					Value: hmacSecret,
				},
			},
			req:       reqHMAC256,
			bodyBytes: hmacBody,
			expected:  true,
		},
		{
			name: "HMAC-SHA256 Hex Match No Prefix",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256,
					Key:   "X-Gitea-Signature",
					Value: hmacSecret,
				},
			},
			req:       reqHMAC256NoPrefix,
			bodyBytes: hmacBody,
			expected:  true,
		},
		{
			name: "HMAC-SHA256 Base64 Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256Base64,
					Key:   "X-Shopify-Hmac-SHA256",
					Value: hmacSecret,
				},
			},
			req:       reqHMAC256Base64,
			bodyBytes: hmacBody,
			expected:  true,
		},
		{
			name: "HMAC-SHA1 Hex Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA1,
					Key:   "X-Hub-Signature",
					Value: hmacSecret,
				},
			},
			req:       reqHMAC1,
			bodyBytes: hmacBody,
			expected:  true,
		},
		{
			name: "HMAC-SHA1 Base64 Match",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA1Base64,
					Key:   "X-Signature",
					Value: hmacSecret,
				},
			},
			req:       reqHMAC1Base64,
			bodyBytes: hmacBody,
			expected:  true,
		},
		{
			name: "HMAC Mismatch Signature",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256,
					Key:   "X-Hub-Signature-256",
					Value: hmacSecret,
				},
			},
			req:       reqHMACBadSig,
			bodyBytes: hmacBody,
			expected:  false,
		},
		{
			name: "HMAC Missing Signature Header",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256,
					Key:   "X-Hub-Signature-256",
					Value: hmacSecret,
				},
			},
			req:       reqHMACMissingHeader,
			bodyBytes: hmacBody,
			expected:  false,
		},
		{
			name: "HMAC Mismatch Secret",
			webhook: &Webhook{
				Verification: ValidationType{
					Type:  TypeHMACSHA256,
					Key:   "X-Hub-Signature-256",
					Value: "different-secret",
				},
			},
			req:       reqHMAC256,
			bodyBytes: hmacBody,
			expected:  false,
		},
		{
			name: "Unknown verification type",
			webhook: &Webhook{
				Verification: ValidationType{
					Type: "invalid",
				},
			},
			req:      req,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rCopy := tt.req
			got := s.isValid(tt.webhook, &rCopy, msg, tt.bodyBytes)
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

func TestWebhookHandlerSWT(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	var receivedTelegramRequest bool
	var receivedMessageText string

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.telegram.org" && strings.Contains(req.URL.Path, "/botfake-token/sendMessage") {
			receivedTelegramRequest = true

			var payload map[string]any
			_ = json.NewDecoder(req.Body).Decode(&payload)
			receivedMessageText = payload["text"].(string)

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
  - name: test-swt-webhook
    pattern: test-swt
    contentType: application/json
    parseMode: html
    verification:
      type: swt
      value: "my-swt-secret-key"
    templates:
      - template: "Secure Event: %s received! customer: %s"
        keys:
          - swt:event
          - customer.name
        trigger:
          type: message
          key: swt:event
          value: payment.completed
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

	payload := []byte(`{"customer":{"name":"Alice"}}`)
	swtToken, err := generateSWTToken("payment.completed", payload, []byte("my-swt-secret-key"))
	if err != nil {
		t.Fatalf("failed to generate SWT token: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/webhooks/test-swt", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+swtToken)

	s.mux.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", recorder.Code, recorder.Body.String())
	}

	if !receivedTelegramRequest {
		t.Error("expected Telegram API call, but none was received")
	}

	expectedMsg := "Secure Event: payment.completed received! customer: Alice"
	if receivedMessageText != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, receivedMessageText)
	}
}
