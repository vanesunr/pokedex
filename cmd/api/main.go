// Command api runs the Pokedex HTTP server.
//
// Startup order: connect to PostgreSQL, Migrate (create the tables if
// missing), wire Repository -> Service -> Handler, then listen. It does not
// load the data; that is done by cmd/seed. On SIGINT/SIGTERM it stops
// accepting requests and lets the running ones finish.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pokedex/internal/adapter/httpapi"
	"pokedex/internal/adapter/postgres"
	"pokedex/internal/app"
)

// main creates the JSON logger, runs the server and exits with a non-zero
// status if it fails.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run connects to the database, applies migrations, builds the dependency
// chain and serves HTTP until the server fails or a shutdown signal arrives,
// in which case it drains in-flight requests (up to 10 seconds).
func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://pokedex:pokedex@localhost:5432/pokedex?sslmode=disable"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	svc := app.NewService(postgres.NewRepository(pool))
	srv := &http.Server{
		Addr:              env("HTTP_ADDR", ":8080"),
		Handler:           httpapi.NewHandler(svc, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// env returns the value of the environment variable key, or def if it is
// unset or empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
