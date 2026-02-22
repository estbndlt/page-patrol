package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"page-patrol/internal/config"
	"page-patrol/internal/db"
	"page-patrol/internal/email"
	"page-patrol/internal/repository"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	repo := repository.New(database)
	sender := email.NewSMTPSender(cfg)
	worker := email.NewWorker(repo, sender, log.New(os.Stdout, "[worker] ", log.LstdFlags|log.Lshortfile))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := worker.Run(ctx); err != nil {
		log.Fatalf("worker run error: %v", err)
	}
}
