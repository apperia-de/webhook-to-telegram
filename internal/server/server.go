package server

import (
	"encoding/json"
	"fmt"
	"github.com/NicoNex/echotron/v3"
	"github.com/sknr/webhook-to-telegram/internal/telegram"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
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

type Config struct {
	Telegram Telegram   `yaml:"telegram"`
	Webhooks []*Webhook `yaml:"webhooks"`
}

type Webhook struct {
	Name           string             `yaml:"name"`
	Pattern        string             `yaml:"pattern"`
	ContentType    string             `yaml:"contentType"`
	FormKey        string             `yaml:"formKey"`
	ParseMode      echotron.ParseMode `yaml:"parseMode"`
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
}

func New() *WebhookServer {
	mux := http.NewServeMux()
	httpServer := &http.Server{Addr: ":8080", Handler: mux}
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	s := &WebhookServer{
		httpServer: httpServer,
		mux:        mux,
		config:     &Config{},
	}

	s.initialize()
	return s
}

func (s *WebhookServer) GetHttpServer() *http.Server {
	return s.httpServer
}

func (s *WebhookServer) Start() {
	t := telegram.New(s.config.Telegram.BotToken)
	dsp := echotron.NewDispatcher(s.config.Telegram.BotToken, t.NewBot)
	dsp.SetHTTPServer(s.GetHttpServer())
	log.Println(dsp.ListenWebhook(s.config.Telegram.WebhookURL))
}

func (s *WebhookServer) initialize() {
	data, err := os.ReadFile(ConfigFile)

	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(data, s.config)
	if err != nil {
		panic(err)
	}

	if s.config.Telegram.ChatID == nil {
		panic("Required telegram chatID is missing!")
	}

	s.createWebhookHandlers(s.config.Webhooks)
}

func (s *WebhookServer) createWebhookHandlers(webhooks []*Webhook) {
	for _, wh := range webhooks {
		log.Println("Creating Webhook", wh.Name)
		// Create a local copy of the variable otherwise the last value is always used on the call of handleWebhook.
		wh := wh
		handleWebhook := func(w http.ResponseWriter, r *http.Request) {
			var (
				data string
				err  error
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
				if wh.FormKey == "" {
					wh.FormKey = DefaultFormKey
				}

				if !r.Form.Has(wh.FormKey) {
					log.Printf("FormKey: %q does not exist.", wh.FormKey)
				}

				data = r.Form.Get(wh.FormKey)
			case HeaderJSON:
				body, err := io.ReadAll(r.Body)
				if err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				data = string(body)
			default:
				log.Printf("Unsupported ContentType: %s", headerContentType)
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}

			// Log received data
			log.Println(data)

			var msg map[string]interface{}

			err = json.Unmarshal([]byte(data), &msg)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if !s.isValid(wh, r, msg) {
				log.Printf("Invalid webhook message!")
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}

			var values []any

			t, err := findMatchingTemplate(r, msg, wh.Templates)
			if err != nil {
				log.Println(err)
				// Default status 204
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// No matching template found
			if t == nil {
				// Default status 204
				w.WriteHeader(http.StatusNoContent)
				return
			}

			for _, key := range t.Keys {
				val := getValue(key, msg, r.Header)
				if val == nil {
					values = append(values, "")
					continue
				}
				values = append(values, val)
			}

			text := fmt.Sprintf(t.Template, values...)

			api := echotron.NewAPI(s.config.Telegram.BotToken)

			if wh.ParseMode != "" {
				log.Println("send message in parseMode: ", wh.ParseMode)
			}
			switch wh.ParseMode {
			case echotron.HTML:
				_, err = api.SendMessage(s.escapeText(echotron.HTML, text), s.getChatID(wh), &echotron.MessageOptions{
					ParseMode: echotron.HTML,
				})
			case echotron.Markdown:
				_, err = api.SendMessage(s.escapeText(echotron.Markdown, text), s.getChatID(wh), &echotron.MessageOptions{
					ParseMode: echotron.Markdown,
				})
			case echotron.MarkdownV2:
				_, err = api.SendMessage(s.escapeText(echotron.MarkdownV2, text), s.getChatID(wh), &echotron.MessageOptions{
					ParseMode: echotron.MarkdownV2,
				})
			default:
				_, err = api.SendMessage(text, s.getChatID(wh), nil)
			}

			if err != nil {
				log.Println("cannot send telegram message:", err)
				// Default status 204
				w.WriteHeader(http.StatusNoContent)
			}
		}

		s.mux.HandleFunc(fmt.Sprintf("/webhooks/%s", wh.Pattern), handleWebhook)
	}
}

// findMatchingTemplate Returns the template which matches the trigger first or the first which has no trigger at all
func findMatchingTemplate(r *http.Request, msg map[string]interface{}, templates []*Template) (*Template, error) {
	var triggerVal string

	for _, t := range templates {
		// If there is no trigger, we took the template immediately (first match wins
		if t.Trigger == nil {
			return t, nil
		}
		switch t.Trigger.Type {
		case TypeHeader:
			triggerVal = r.Header.Get(t.Trigger.Key)
		case TypeMessage:
			triggerVal, _ = getValue(t.Trigger.Key, msg, r.Header).(string)
		default:
			return nil, fmt.Errorf("unkown type: %q", t.Trigger.Type)
		}

		if triggerVal == t.Trigger.Value {
			return t, nil
		}
	}

	return nil, nil
}

func (s *WebhookServer) isValid(wh *Webhook, r *http.Request, msg map[string]interface{}) bool {
	var val string
	switch wh.Verification.Type {
	case TypeNone:
		return true
	case TypeMessage:
		val, _ = getValue(wh.Verification.Key, msg, r.Header).(string)
	case TypeHeader:
		val = r.Header.Get(wh.Verification.Key)
	default:
		return false
	}
	return val == wh.Verification.Value
}

func getValue(key string, msg map[string]interface{}, header http.Header) interface{} {
	var (
		val any
		ok  bool
	)

	// Check if the value should be in a custom header.
	if strings.Contains(key, "header:") {
		parts := strings.Split(key, "header:")
		if len(parts) == 2 {
			return header.Get(parts[1])
		}
		return nil
	}

	// Take the value from the message itself.
	for i, nestedKey := range strings.Split(key, ".") {
		if i == 0 {
			val, ok = msg[nestedKey]
			if !ok {
				log.Printf("Nested key %q does not exist!", nestedKey)
				break
			}
			continue
		}

		switch nextVal := val.(type) {
		case map[string]interface{}:
			if val, ok = nextVal[nestedKey]; !ok {
				log.Printf("Nested key %q does not exist!", nestedKey)
				break
			}
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
func (s *WebhookServer) escapeText(parseMode echotron.ParseMode, text string) string {
	var replacer *strings.Replacer

	switch parseMode {
	case echotron.HTML:
		replacer = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	case echotron.Markdown:
		replacer = strings.NewReplacer("_", "\\_", "*", "\\*", "`", "\\`", "[", "\\[")
	case echotron.MarkdownV2:
		replacer = strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
			"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
			"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
			"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
		)
	default:
		return ""
	}

	return replacer.Replace(text)
}
