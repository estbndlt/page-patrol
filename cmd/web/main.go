package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"page-patrol/internal/config"
	"page-patrol/internal/db"
	"page-patrol/internal/repository"
	"page-patrol/internal/web"
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

	ctx := context.Background()
	if err := db.Migrate(ctx, database, cfg.MigrationsDir); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	repo := repository.New(database)
	if err := repo.EnsureCoordinatorInvite(ctx, cfg.CoordinatorEmail); err != nil {
		log.Fatalf("ensure coordinator invite: %v", err)
	}

	server, err := web.NewServer(cfg, repo, log.New(os.Stdout, "[web] ", log.LstdFlags|log.Lshortfile))
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("Page Patrol web listening on %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
