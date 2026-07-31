package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"notes-server/internal/auth"
	"notes-server/internal/config"
	"notes-server/internal/devices"
	"notes-server/internal/httpapi"
	apimw "notes-server/internal/middleware"
	"notes-server/internal/notes"
	"notes-server/internal/store"
	syncapi "notes-server/internal/sync"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.LogLevel)

	st, err := store.Open(ctx, cfg)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	verifier, err := auth.NewVerifier(ctx, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTJWKSURL)
	if err != nil {
		logger.Error("initialize jwt verifier", "error", err)
		os.Exit(1)
	}

	router := buildRouter(cfg, logger, st, verifier)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.RequestTimeout + 5*time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting api", "addr", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("api stopped")
}

func buildRouter(cfg config.Config, logger *slog.Logger, st *store.Store, verifier *auth.Verifier) http.Handler {
	deviceService := devices.NewService(st)
	deviceHandler := devices.NewHandler(deviceService)
	noteService := notes.NewService(st)
	noteHandler := notes.NewHandler(noteService)
	syncService := syncapi.NewService(st, cfg, logger)
	syncHandler := syncapi.NewHandler(syncService)

	r := chi.NewRouter()
	r.Use(apimw.RequestID)
	r.Use(apimw.Recovery(logger))
	r.Use(apimw.RequestLogging(logger))
	r.Use(apimw.Timeout(cfg.RequestTimeout))
	r.Use(apimw.RateLimit(apimw.AllowAllLimiter{}))
	r.Use(apimw.BodyLimit(cfg.MaxCompressedRequestBytes))
	r.Use(apimw.GzipDecompress(cfg.MaxDecompressedRequestBytes))
	r.Use(apimw.GzipResponse)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, httpapi.CodeInternalError, "Database is not ready."))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.Middleware(verifier))
		r.Mount("/devices", deviceHandler.Routes())
		r.Post("/sync", syncHandler.ServeHTTP)
		r.Mount("/notes", noteHandler.Routes())
	})
	return r
}

func newLogger(level string) *slog.Logger {
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
