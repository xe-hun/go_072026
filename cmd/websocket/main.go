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
	"notes-server/internal/httpapi"
	apimw "notes-server/internal/middleware"
	"notes-server/internal/realtime"
	"notes-server/internal/store"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// The WebSocket process has its own pool because it is a separate process
	// from the API. The pool is used only while establishing a connection to
	// validate the authenticated device.
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

	hub := realtime.NewHub(logger)
	eventBus := realtime.NewPostgresEventBus(st.Pool(), logger)
	listenerCtx, cancelListener := context.WithCancel(context.Background())
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		if err := eventBus.Subscribe(listenerCtx, hub.PublishSyncAvailable); err != nil {
			logger.Error("postgres realtime listener stopped", "error", err)
		}
	}()

	router := buildRouter(cfg, logger, st, verifier, hub)
	server := &http.Server{
		Addr:              ":" + cfg.WebSocketPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting websocket server", "addr", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var serverErr error
	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("websocket server failed", "error", err)
			serverErr = err
		}
	}
	signal.Stop(stop)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	cancelListener()
	<-listenerDone
	hub.Close()
	logger.Info("websocket server stopped")
	if serverErr != nil {
		os.Exit(1)
	}
}

func buildRouter(cfg config.Config, logger *slog.Logger, st *store.Store, verifier *auth.Verifier, hub *realtime.Hub) http.Handler {
	realtimeHandler := realtime.NewHandler(hub, st, logger)

	r := chi.NewRouter()
	// WebSocket requests intentionally do not use the regular HTTP timeout
	// middleware: the upgraded connection is expected to remain open.
	r.Use(apimw.RequestID)
	r.Use(apimw.Recovery(logger))
	r.Use(apimw.RateLimit(apimw.AllowAllLimiter{}))
	r.Use(apimw.BodyLimit(cfg.MaxCompressedRequestBytes))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			logger.ErrorContext(r.Context(), "database readiness check failed",
				"request_id", httpapi.RequestIDFromContext(r.Context()),
				"error", err,
			)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, httpapi.CodeInternalError, "Database is not ready."))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.Middleware(verifier))
		r.Get("/realtime", realtimeHandler.ServeHTTP)
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
