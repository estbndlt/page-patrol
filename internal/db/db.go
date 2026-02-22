package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"page-patrol/internal/config"
)

func Open(cfg config.Config) (*sql.DB, error) {
	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}

	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetMaxIdleConns(5)
	database.SetMaxOpenConns(20)

	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("sql ping: %w", err)
	}
	return database, nil
}
