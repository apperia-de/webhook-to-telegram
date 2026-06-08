package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/sknr/webhook-to-telegram/internal/telegram"
	"gopkg.in/yaml.v3"
)

const (
	ConfigFile = "config.yml"

	DefaultFormKey = "payload"

	HeaderFormURLEncoded = "application/x-www-form-urlencoded"
	HeaderJSON           = "application/json"

	TypeNone    = "none"
	TypeHeader  = "header"
	TypeMessage = "message"
)

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
	Name           string             `yaml:"name"`
	Pattern        string             `yaml:"pattern"`
	ContentType    string             `yaml:"contentType"`
	FormKey        string             `yaml:"formKey"`
	ParseMode      telegram.ParseMode `yaml:"parseMode"`
	TelegramChatID *int64             `yaml:"telegramChatID,omitempty"`
	Verification   ValidationType     `yaml:"verification"`
	Templates      []*Template        `yaml:"templates,omitempty"`
}

type Telegram struct {
	BotToken   string `yaml:"botToken"`
	ChatID     *int64 `yaml:"chatID"`
	WebhookURL string `yaml:"webhookURL"`
}

type Template struct {
	Template string       `yaml:"template"`
	Keys     []string     `yaml:"keys"`
	Trigger  *TriggerType `yaml:"trigger,omitempty"`
}

type ValidationType struct {
	Type  string `yaml:"type"` // Either header or message
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
}

func New() (*WebhookServer, error) {
	mux := http.NewServeMux()
	httpServer := &http.Server{Addr: ":8080", Handler: mux}
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	s := &WebhookServer{
		httpServer: httpServer,
		mux:        mux,
		config:     &Config{},
	}

	if err := s.initialize(); err != nil {
		return nil, err
	}
	return s, nil
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
			if err := s.api.SendMessage(chatID, msgText, ""); err != nil {
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
				log.Printf("Unsupported ContentType: %s", headerContentType)
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}

			// Log received data
			log.Println(string(dataBytes))

			var msg map[string]any

			err = json.Unmarshal(dataBytes, &msg)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if !s.isValid(wh, r, msg) {
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
				val := getValue(key, msg, r.Header)
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

			err = s.api.SendMessage(s.getChatID(wh), text, wh.ParseMode)
			if err != nil {
				log.Println("cannot send telegram message:", err)
				w.WriteHeader(http.StatusBadGateway)
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
			val := getValue(t.Trigger.Key, msg, r.Header)
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

func (s *WebhookServer) isValid(wh *Webhook, r *http.Request, msg map[string]any) bool {
	var val string
	switch wh.Verification.Type {
	case TypeNone:
		return true
	case TypeMessage:
		valVal := getValue(wh.Verification.Key, msg, r.Header)
		if valVal == nil {
			val = ""
		} else {
			val = fmt.Sprintf("%v", valVal)
		}
	case TypeHeader:
		val = r.Header.Get(wh.Verification.Key)
	default:
		return false
	}
	return subtle.ConstantTimeCompare([]byte(val), []byte(wh.Verification.Value)) == 1
}

func getValue(key string, msg map[string]any, header http.Header) any {
	// Check if the value should be in a custom header.
	if strings.Contains(key, "header:") {
		parts := strings.Split(key, "header:")
		if len(parts) == 2 {
			return header.Get(parts[1])
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

// EscapeText takes an input text and escape Telegram markup symbols.
// In this way we can send a text without being afraid of having to escape the characters manually.
// Note that you don't have to include the formatting style in the input text, or it will be escaped too.
// If there is an error, an empty string will be returned.
//
// parseMode is the text formatting mode (ModeMarkdown, ModeMarkdownV2 or ModeHTML)
// text is the input string that will be escaped
func (s *WebhookServer) escapeText(parseMode telegram.ParseMode, text string) string {
	switch parseMode {
	case telegram.HTML:
		return htmlReplacer.Replace(text)
	case telegram.Markdown:
		return markdownReplacer.Replace(text)
	case telegram.MarkdownV2:
		return markdownV2Replacer.Replace(text)
	default:
		return ""
	}
}
