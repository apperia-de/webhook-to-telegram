package server

import (
	"encoding/json"
	"fmt"
	"github.com/NicoNex/echotron/v3"
	"github.com/sknr/webhookbot/internal/telegram"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net/http"
	"os"
)

const (
	HeaderFormURLEncoded = "application/x-www-form-urlencoded"
	HeaderJSON           = "application/json"
)

type WebhookConfig struct {
	Name            string   `yaml:"name"`
	Pattern         string   `yaml:"pattern"`
	Token           string   `yaml:"token"`
	TokenMessageKey string   `yaml:"tokenMessageKey"`
	ContentType     string   `yaml:"contentType"`
	FormKey         string   `yaml:"formKey"`
	TelegramChatID  int64    `yaml:"telegramChatID"`
	MessageKeys     []string `yaml:"messageKeys"`
	MessageTemplate string   `yaml:"messageTemplate"`
}

type WebhookServer struct {
	httpServer *http.Server
	mux        *http.ServeMux
	botToken   string
}

func New(botToken string) *WebhookServer {
	mux := http.NewServeMux()
	httpServer := &http.Server{Addr: ":8080", Handler: mux}

	s := &WebhookServer{
		httpServer: httpServer,
		mux:        mux,
		botToken:   botToken,
	}

	s.initialize()
	return s
}

func (s *WebhookServer) GetHttpServer() *http.Server {
	return s.httpServer
}

func (s *WebhookServer) Start() {
	dsp := echotron.NewDispatcher(os.Getenv("BOT_TOKEN"), telegram.NewBot)
	dsp.SetHTTPServer(s.GetHttpServer())
	log.Println(dsp.ListenWebhook(os.Getenv("TELEGRAM_WEBHOOK_URL")))
}

func (s *WebhookServer) initialize() {
	data, err := os.ReadFile("webhooks.yml")

	if err != nil {
		log.Fatal(err)
	}

	var wh []WebhookConfig

	err = yaml.Unmarshal(data, &wh)
	if err != nil {
		fmt.Println(err)
	}

	s.createWebhookHandlers(wh)
}

func (s *WebhookServer) createWebhookHandlers(webhooks []WebhookConfig) {
	for _, wh := range webhooks {
		log.Println("Creating Webhook", wh.Name)

		handleWebhook := func(w http.ResponseWriter, r *http.Request) {
			var data string

			if r.Method != http.MethodPost {
				log.Println("Method not allowed:", r.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			headerContentType := r.Header.Get("Content-Type")
			switch headerContentType {
			case HeaderFormURLEncoded:
				if err := r.ParseForm(); err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				data = r.Form.Get(wh.FormKey)
			case HeaderJSON:
				body, _ := io.ReadAll(r.Body)
				data = string(body)
			default:
				log.Printf("Unsupported ContentType: %s", headerContentType)
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}

			// Log received data
			log.Println(data)

			var msg map[string]interface{}

			err := json.Unmarshal([]byte(data), &msg)
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			var values []any

			for _, key := range wh.MessageKeys {
				if val, ok := msg[key]; ok {
					values = append(values, val.(string))
				}
			}

			text := fmt.Sprintf(wh.MessageTemplate, values...)
			messageToken, ok := msg[wh.TokenMessageKey]
			if !ok {
				log.Println("Missing message verification token")
				http.Error(w, err.Error(), http.StatusBadRequest)
			}

			if messageToken == wh.Token {
				api := echotron.NewAPI(s.botToken)
				_, _ = api.SendMessage(text, wh.TelegramChatID, nil)
			}
		}

		s.mux.HandleFunc(fmt.Sprintf("/webhooks/%s", wh.Pattern), handleWebhook)
	}
}
