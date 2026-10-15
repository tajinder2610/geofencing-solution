// Command server runs the geofencing & real-time alert API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"geofence-backend/internal/api"
	"geofence-backend/internal/config"
	"geofence-backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Wait up to a minute for the database (useful under docker compose).
	dbCtx, cancel := context.WithTimeout(ctx, time.Minute)
	st, err := store.Open(dbCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	srv := api.NewServer(cfg, st)
	go srv.RunAlertListener(ctx)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s (auth=%t, rate limit=%.0f rps)", cfg.Port, cfg.APIKey != "", cfg.RateLimitRPS)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Println("shutting down")
	srv.Hub().Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
