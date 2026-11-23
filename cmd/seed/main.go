// Command seed creates the schema (if needed) and loads pokemons.json into
// PostgreSQL. It is idempotent and safe to run repeatedly.
//
// Order of work: parse the file (jsonfile), connect, Migrate (create the
// tables if missing), then Seed (insert the rows). The file is parsed first
// so that a bad file fails before the database is touched.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pokedex/internal/adapter/jsonfile"
	"pokedex/internal/adapter/postgres"
)

// main creates the text logger, runs the seeding and exits with a non-zero
// status if it fails.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

// run parses the JSON file, then migrates and seeds the database, all within
// a 2 minute timeout.
func run(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	path := env("POKEMON_FILE", "pokemons.json")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	pokemons, err := jsonfile.Parse(f)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://pokedex:pokedex@localhost:5432/pokedex?sslmode=disable"))
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}
	if err := postgres.Seed(ctx, pool, pokemons); err != nil {
		return err
	}
	logger.Info("seeded", "pokemons", len(pokemons), "file", path)
	return nil
}

// env returns the value of the environment variable key, or def if it is
// unset or empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
