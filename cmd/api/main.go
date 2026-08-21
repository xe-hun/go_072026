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
	// The API process is intentionally stateless. Startup gathers all durable
	// dependencies up front, then passes them through constructors so packages do
	// not depend on global database or logger variables.
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// Store owns the pgx connection pool and the sqlc query wrapper. The pool is
	// created once per process and closed during graceful shutdown.
	st, err := store.Open(ctx, cfg)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// The verifier downloads the JWKS at startup so a bad auth configuration is
	// detected before the server accepts traffic.
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
		// ListenAndServe blocks, so it runs in a goroutine while the main goroutine
		// waits for either a process signal or a server error.
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
	// Shutdown stops accepting new connections and gives in-flight requests a
	// short window to finish before the process exits.
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("api stopped")
}

func buildRouter(cfg config.Config, logger *slog.Logger, st *store.Store, verifier *auth.Verifier) http.Handler {
	// Services contain business rules; handlers only decode/encode HTTP data.
	// This mirrors the intended flow: handler -> service -> store/sqlc.
	deviceService := devices.NewService(st)
	deviceHandler := devices.NewHandler(deviceService)
	noteService := notes.NewService(st)
	noteHandler := notes.NewHandler(noteService)
	syncService := syncapi.NewService(st, cfg, logger)
	syncHandler := syncapi.NewHandler(syncService)

	r := chi.NewRouter()
	// Global middleware applies to both public and authenticated routes. Request
	// ID is first so later middleware and error responses can include it.
	r.Use(apimw.RequestID)
	r.Use(apimw.Recovery(logger))
	r.Use(apimw.RequestLogging(logger))
	r.Use(apimw.Timeout(cfg.RequestTimeout))
	r.Use(apimw.RateLimit(apimw.AllowAllLimiter{}))
	r.Use(apimw.BodyLimit(cfg.MaxCompressedRequestBytes))
	// r.Use(apimw.GzipDecompress(cfg.MaxDecompressedRequestBytes))
	// r.Use(apimw.GzipResponse)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		// Health is process-only and does not touch PostgreSQL. It is suitable for
		// container liveness checks.
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Readiness confirms the process can currently reach PostgreSQL.
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
		// All versioned product routes require a valid bearer token. The middleware
		// writes auth.Claims to context for downstream handlers/services.
		r.Use(auth.Middleware(verifier))
		r.Mount("/devices", deviceHandler.Routes())
		r.Post("/sync", syncHandler.ServeHTTP)
		r.Get("/sync", syncHandler.PullHTTP)
		r.Mount("/notes", noteHandler.Routes())
	})
	return r
}

func newLogger(level string) *slog.Logger {
	// slog's JSON handler gives structured logs that can be shipped directly to
	// Cloud Run or another container log collector.
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
