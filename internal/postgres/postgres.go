// Package postgres owns connection startup and the shared Goose migration runner.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Open verifies connectivity within the caller's startup budget.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid database connection configuration")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("database startup: %w", ctx.Err())
		}
		return nil, errors.New("database startup connection failed")
	}
	return pool, nil
}

// Migrate applies the same runner to production migrations and isolated test fixtures.
func Migrate(ctx context.Context, url string, source fs.FS, direction string) error {
	if direction != "up" && direction != "down" {
		return errors.New("migration direction must be up or down")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return errors.New("invalid migration database configuration")
	}
	defer func() { _ = db.Close() }()
	if err = db.PingContext(ctx); err != nil {
		return errors.New("migration database unavailable")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, source, goose.WithSlog(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	if errors.Is(err, goose.ErrNoMigrations) && direction == "up" {
		return nil
	}
	if err != nil {
		return errors.New("migration source invalid")
	}
	if direction == "up" {
		_, err = provider.Up(ctx)
	} else {
		_, err = provider.Down(ctx)
	}
	if err != nil {
		return errors.New("database migration failed; inspect the migration SQL and database state")
	}
	return nil
}
