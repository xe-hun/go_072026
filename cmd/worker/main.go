package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"notes-server/internal/config"
	"notes-server/internal/jobs"
	"notes-server/internal/store"
)

func main() {
	// NotifyContext cancels the root context on Ctrl+C or SIGTERM. The worker
	// loop observes this context between jobs so shutdown is cooperative.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// The worker has its own database pool because it is a separate process from
	// the API. It still uses the same store/sqlc persistence boundary.
	st, err := store.Open(ctx, cfg)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// An empty worker ID tells NewWorker to generate a stable process-local ID for
	// lock ownership logs and outbox job claims.
	worker := jobs.NewWorker(st, logger, "")
	logger.Info("starting worker")
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("worker stopped")
}

func newLogger(level string) *slog.Logger {
	// Keep worker logging behavior aligned with the API so both processes produce
	// the same JSON log shape.
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
