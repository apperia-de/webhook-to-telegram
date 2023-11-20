package telegram

import (
	"fmt"
	"github.com/NicoNex/echotron/v3"
	"github.com/joho/godotenv"
	"os"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}
}

func NewBot(chatID int64) echotron.Bot {
	return &bot{
		chatID,
		echotron.NewAPI(os.Getenv("BOT_TOKEN")),
	}
}

type bot struct {
	chatID int64
	echotron.API
}

func (b *bot) Update(update *echotron.Update) {
	// Currently the only command we serve is the /id in order to show the users their own chatID.
	if update.Message.Text == "/id" {
		_, _ = b.SendMessage(fmt.Sprintf("Your ChatID is: %d", b.chatID), b.chatID, nil)
	}
}
