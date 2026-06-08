package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sknr/webhook-to-telegram/internal/server"
)

func main() {
	termChan := make(chan os.Signal, 1) // Channel for terminating the app via os.Interrupt signal
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	s, err := server.New()
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	go func() {
		<-termChan
		// Perform some cleanup...
		log.Println("Shutting down server gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.GetHttpServer().Shutdown(ctx); err != nil {
			log.Print(err)
		}
	}()
	s.Start()
}
