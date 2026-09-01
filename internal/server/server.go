package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sknr/webhook-to-telegram/internal/swt"
	"github.com/sknr/webhook-to-telegram/internal/telegram"
	"gopkg.in/yaml.v3"
)

const (
	ConfigFile = "config.yml"

	DefaultFormKey = "payload"

	// sendQueueSize bounds how many outgoing messages may be waiting for
	// delivery before new webhook events are rejected.
	sendQueueSize = 1024
	// perChatSendInterval paces messages to the same chat, keeping us below
	// Telegram's rate limit of ~20 messages per minute per group.
	perChatSendInterval = 3 * time.Second

	HeaderFormURLEncoded = "application/x-www-form-urlencoded"
	HeaderJSON           = "application/json"

	TypeNone             = "none"
	TypeHeader           = "header"
	TypeMessage          = "message"
	TypeHMACSHA256       = "hmac-sha256"
	TypeHMACSHA1         = "hmac-sha1"
	TypeHMACSHA256Base64 = "hmac-sha256-base64"
	TypeHMACSHA1Base64   = "hmac-sha1-base64"
)

type contextKey string

const swtClaimsKey contextKey = "swtClaims"

var (
	htmlReplacer       = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	markdownReplacer   = strings.NewReplacer("_", "\\_", "*", "\\*", "`", "\\`", "[", "\\[")
	markdownV2Replacer = strings.NewReplacer(
		"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)",
		"~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
		"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
	)
)

type Config struct {
	Telegram Telegram   `yaml:"telegram"`
	Webhooks []*Webhook `yaml:"webhooks"`
}

type Webhook struct {
	Name                       string             `yaml:"name"`
	Pattern                    string             `yaml:"pattern"`
	ContentType                string             `yaml:"contentType"`
	FormKey                    string             `yaml:"formKey"`
	ParseMode                  telegram.ParseMode `yaml:"parseMode"`
	TelegramChatID             *int64             `yaml:"telegramChatID,omitempty"`
	TelegramMessageThreadID    *int64             `yaml:"telegramMessageThreadID,omitempty"`
	TelegramDisableLinkPreview *bool              `yaml:"telegramDisableLinkPreview,omitempty"`
	Verification               ValidationType     `yaml:"verification"`
	Templates                  []*Template        `yaml:"templates,omitempty"`
}

type Telegram struct {
	BotToken        string `yaml:"botToken"`
	ChatID          *int64 `yaml:"chatID"`
	MessageThreadID *int64 `yaml:"messageThreadID"`
	// DisableLinkPreview disables Telegram link previews for all outgoing
	// messages (sets link_preview_options.is_disabled on sendMessage).
	DisableLinkPreview bool   `yaml:"disableLinkPreview"`
	WebhookURL         string `yaml:"webhookURL"`
}

type Template struct {
	Template string       `yaml:"template"`
	Keys     []string     `yaml:"keys"`
	Trigger  *TriggerType `yaml:"trigger,omitempty"`
}

type ValidationType struct {
	Type  string `yaml:"type"` // Either header, message or swt
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type TriggerType struct {
	Type  string `yaml:"type"` // Either header or message
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type WebhookServer struct {
	httpServer *http.Server
	mux        *http.ServeMux
	config     *Config
	api        *telegram.Client
	sendQueue  chan sendRequest
}

// sendRequest is a queued outgoing Telegram message.
type sendRequest struct {
	chatID int64
	text   string
	opts   *telegram.SendMessageOptions
}

func New() (*WebhookServer, error) {
	mux := http.NewServeMux()
	httpServer := &http.Server{Addr: ":8080", Handler: mux}
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	s := &WebhookServer{
		httpServer: httpServer,
		mux:        mux,
		config:     &Config{},
		sendQueue:  make(chan sendRequest, sendQueueSize),
	}

	if err := s.initialize(); err != nil {
		return nil, err
	}

	go s.processSendQueue()

	return s, nil
}

// processSendQueue serializes all outgoing Telegram messages so that bursts of
// webhook events are spread out instead of being dropped by Telegram's rate
// limit, and paces messages per chat. SendMessage additionally honors the
// retry_after duration on HTTP 429 responses.
func (s *WebhookServer) processSendQueue() {
	lastSent := make(map[int64]time.Time)
	for req := range s.sendQueue {
		if wait := perChatSendInterval - time.Since(lastSent[req.chatID]); wait > 0 {
			time.Sleep(wait)
		}
		if err := s.api.SendMessage(req.chatID, req.text, req.opts); err != nil {
			log.Println("cannot send telegram message:", err)
		}
		lastSent[req.chatID] = time.Now()
	}
}

func (s *WebhookServer) GetHttpServer() *http.Server {
	return s.httpServer
}

func (s *WebhookServer) Start() {
	u, err := url.Parse(s.config.Telegram.WebhookURL)
	if err != nil {
		log.Fatalf("invalid webhook URL: %v", err)
	}
	path := u.Path
	if path == "" || path == "/" {
		path = "/telegram-webhook"
	}

	// We should register the webhook URL with Telegram
	if err := s.api.SetWebhook(s.config.Telegram.WebhookURL); err != nil {
		log.Printf("Warning: failed to set Telegram webhook: %v", err)
	}

	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var update telegram.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			log.Println("failed to decode telegram update:", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if update.Message != nil && update.Message.Text == "/id" {
			chatID := update.Message.Chat.ID
			msgText := fmt.Sprintf("Your ChatID is: %d", chatID)
			if err := s.api.SendMessage(chatID, msgText, nil); err != nil {
				log.Println("failed to send /id response message:", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Starting HTTP server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func (s *WebhookServer) initialize() error {
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", ConfigFile, err)
	}

	err = yaml.Unmarshal(data, s.config)
	if err != nil {
		return fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if s.config.Telegram.ChatID == nil {
		return fmt.Errorf("required telegram chatID is missing")
	}

	s.api = telegram.NewClient(s.config.Telegram.BotToken)

	for _, wh := range s.config.Webhooks {
		if wh.FormKey == "" {
			wh.FormKey = DefaultFormKey
		}
		switch strings.ToLower(string(wh.ParseMode)) {
		case "html":
			wh.ParseMode = telegram.HTML
		case "markdown":
			wh.ParseMode = telegram.Markdown
		case "markdownv2":
			wh.ParseMode = telegram.MarkdownV2
		}
	}

	s.createWebhookHandlers(s.config.Webhooks)
	return nil
}

func (s *WebhookServer) createWebhookHandlers(webhooks []*Webhook) {
	for _, wh := range webhooks {
		log.Println("Creating Webhook", wh.Name)
		wh := wh
		handleWebhook := func(w http.ResponseWriter, r *http.Request) {
			var (
				dataBytes []byte
				err       error
			)

			if r.Method != http.MethodPost {
				log.Println("Method not allowed:", r.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			headerContentType := r.Header.Get("Content-Type")
			switch headerContentType {
			case HeaderFormURLEncoded:
				if err = r.ParseForm(); err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				if !r.Form.Has(wh.FormKey) {
					log.Printf("FormKey: %q does not exist.", wh.FormKey)
				}

				dataBytes = []byte(r.Form.Get(wh.FormKey))
			case HeaderJSON:
				body, err := io.ReadAll(r.Body)
				if err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				dataBytes = body
			default:
				// SWT might not require JSON content-type if the body is empty,
				// but let's allow empty bodies or fall through.
				if wh.Verification.Type == "swt" && r.ContentLength == 0 {
					dataBytes = nil
				} else {
					log.Printf("Unsupported ContentType: %s", headerContentType)
					w.WriteHeader(http.StatusNotAcceptable)
					return
				}
			}

			// Log received data (only if body is present)
			if len(dataBytes) > 0 {
				log.Println(string(dataBytes))
			}

			var msg map[string]any
			if len(dataBytes) > 0 {
				err = json.Unmarshal(dataBytes, &msg)
				if err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}

			if !s.isValid(wh, &r, msg, dataBytes) {
				log.Printf("Invalid webhook message!")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			t, err := findMatchingTemplate(r, msg, wh.Templates)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// No matching template found
			if t == nil {
				w.WriteHeader(http.StatusOK)
				return
			}

			var values []any
			for _, key := range t.Keys {
				val := getValue(key, msg, r)
				if val == nil {
					values = append(values, "")
					continue
				}
				if strVal, ok := val.(string); ok && wh.ParseMode != "" {
					val = s.escapeText(wh.ParseMode, strVal)
				}
				values = append(values, val)
			}

			text := fmt.Sprintf(t.Template, values...)

			// Queue the message for delivery instead of sending it inline, so
			// slow or rate limited Telegram calls never block webhook responses.
			select {
			case s.sendQueue <- sendRequest{
				chatID: s.getChatID(wh),
				text:   text,
				opts: &telegram.SendMessageOptions{
					ParseMode:          wh.ParseMode,
					MessageThreadID:    s.getMessageThreadID(wh),
					DisableLinkPreview: s.getDisableLinkPreview(wh),
				},
			}:
			default:
				log.Println("send queue full, dropping telegram message")
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			w.WriteHeader(http.StatusOK)
		}

		s.mux.HandleFunc(fmt.Sprintf("/webhooks/%s", wh.Pattern), handleWebhook)
	}
}

// findMatchingTemplate Returns the template which matches the trigger first or the first which has no trigger at all
func findMatchingTemplate(r *http.Request, msg map[string]any, templates []*Template) (*Template, error) {
	var triggerVal string

	for _, t := range templates {
		// If there is no trigger, we took the template immediately (first match wins)
		if t.Trigger == nil {
			return t, nil
		}
		switch t.Trigger.Type {
		case TypeHeader:
			triggerVal = r.Header.Get(t.Trigger.Key)
		case TypeMessage:
			val := getValue(t.Trigger.Key, msg, r)
			if val == nil {
				triggerVal = ""
			} else {
				triggerVal = fmt.Sprintf("%v", val)
			}
		default:
			return nil, fmt.Errorf("unkown type: %q", t.Trigger.Type)
		}

		if triggerVal == t.Trigger.Value {
			return t, nil
		}
	}

	return nil, nil
}

func (s *WebhookServer) isValid(wh *Webhook, r **http.Request, msg map[string]any, body []byte) bool {
	var val string
	switch wh.Verification.Type {
	case TypeNone:
		return true
	case TypeMessage:
		valVal := getValue(wh.Verification.Key, msg, *r)
		if valVal == nil {
			val = ""
		} else {
			val = fmt.Sprintf("%v", valVal)
		}
	case TypeHeader:
		val = (*r).Header.Get(wh.Verification.Key)
	case "swt":
		authHeader := (*r).Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return false
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := swt.Verify(token, []byte(wh.Verification.Value), body, time.Now())
		if err != nil {
			log.Printf("SWT verification failed: %v", err)
			return false
		}
		*r = (*r).WithContext(context.WithValue((*r).Context(), swtClaimsKey, claims))
		return true
	case TypeHMACSHA256, TypeHMACSHA1, TypeHMACSHA256Base64, TypeHMACSHA1Base64:
		return s.verifyHMAC(wh, *r, body)
	default:
		return false
	}
	return subtle.ConstantTimeCompare([]byte(val), []byte(wh.Verification.Value)) == 1
}

func (s *WebhookServer) verifyHMAC(wh *Webhook, r *http.Request, body []byte) bool {
	sigStr := r.Header.Get(wh.Verification.Key)
	if sigStr == "" {
		return false
	}

	// Strip optional prefix (e.g. "sha256=" or "sha1=")
	if strings.HasPrefix(sigStr, "sha256=") {
		sigStr = strings.TrimPrefix(sigStr, "sha256=")
	} else if strings.HasPrefix(sigStr, "sha1=") {
		sigStr = strings.TrimPrefix(sigStr, "sha1=")
	}

	var sig []byte
	var err error
	switch wh.Verification.Type {
	case TypeHMACSHA256, TypeHMACSHA1:
		sig, err = hex.DecodeString(sigStr)
	case TypeHMACSHA256Base64, TypeHMACSHA1Base64:
		sig, err = base64.StdEncoding.DecodeString(sigStr)
	}
	if err != nil {
		log.Printf("Failed to decode signature: %v", err)
		return false
	}

	var mac hash.Hash
	switch wh.Verification.Type {
	case TypeHMACSHA256, TypeHMACSHA256Base64:
		mac = hmac.New(sha256.New, []byte(wh.Verification.Value))
	case TypeHMACSHA1, TypeHMACSHA1Base64:
		mac = hmac.New(sha1.New, []byte(wh.Verification.Value))
	default:
		return false
	}

	mac.Write(body)
	expectedMac := mac.Sum(nil)

	return subtle.ConstantTimeCompare(sig, expectedMac) == 1
}

func getValue(key string, msg map[string]any, r *http.Request) any {
	if key == "swt:event" {
		if claims, ok := r.Context().Value(swtClaimsKey).(*swt.Claims); ok {
			return claims.Webhook.Event
		}
		return nil
	}
	if key == "swt:retry_count" {
		if claims, ok := r.Context().Value(swtClaimsKey).(*swt.Claims); ok && claims.Webhook.RetryCount != nil {
			return *claims.Webhook.RetryCount
		}
		return nil
	}

	// Check if the value should be in a custom header.
	if strings.Contains(key, "header:") {
		parts := strings.Split(key, "header:")
		if len(parts) == 2 {
			return r.Header.Get(parts[1])
		}
		return nil
	}

	var val any = msg
	for _, nestedKey := range strings.Split(key, ".") {
		switch nextVal := val.(type) {
		case map[string]any:
			var ok bool
			val, ok = nextVal[nestedKey]
			if !ok {
				return nil
			}
		default:
			return nil
		}
	}

	return val
}

func (s *WebhookServer) getChatID(wh *Webhook) int64 {
	if wh.TelegramChatID == nil {
		return *s.config.Telegram.ChatID
	}
	return *wh.TelegramChatID
}

func (s *WebhookServer) getMessageThreadID(wh *Webhook) *int64 {
	if wh.TelegramMessageThreadID == nil {
		return s.config.Telegram.MessageThreadID
	}
	return wh.TelegramMessageThreadID
}

func (s *WebhookServer) getDisableLinkPreview(wh *Webhook) bool {
	if wh.TelegramDisableLinkPreview == nil {
		return s.config.Telegram.DisableLinkPreview
	}
	return *wh.TelegramDisableLinkPreview
}

// EscapeText takes an input text and escape Telegram markup symbols.
// In this way we can send a text without being afraid of having to escape the characters manually.
// Note that you don't have to include the formatting style in the input text, or it will be escaped too.
// If there is an error, an empty string will be returned.
//
// parseMode is the text formatting mode (ModeMarkdown, ModeMarkdownV2 or ModeHTML)
// text is the input string that will be escaped
func (s *WebhookServer) escapeText(parseMode telegram.ParseMode, text string) string {
	switch strings.ToLower(string(parseMode)) {
	case "html":
		return htmlReplacer.Replace(text)
	case "markdown":
		return markdownReplacer.Replace(text)
	case "markdownv2":
		return markdownV2Replacer.Replace(text)
	default:
		return ""
	}
}
