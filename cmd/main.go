package main

import (
	"context"
	"github.com/joho/godotenv"
	"github.com/sknr/webhook-to-telegram/internal/server"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}

	termChan := make(chan os.Signal, 1) // Channel for terminating the app via os.Interrupt signal
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	s := server.New(os.Getenv("BOT_TOKEN"))
	go func() {
		<-termChan
		// Perform some cleanup...
		if err := s.GetHttpServer().Shutdown(context.Background()); err != nil {
			log.Print(err)
		}
	}()
	s.Start()
}
