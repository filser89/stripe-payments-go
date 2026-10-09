package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/postgres/queries"
	"github.com/filser89/stripe-payments-go/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("service failed", "error", err.Error())
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, getenv func(string) string, output io.Writer) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if command != "serve" && command != "migrate" && command != "probe" {
		return errors.New("unknown command; use serve, migrate, or probe")
	}
	c, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if command == "serve" {
		if err := c.ValidateServing(); err != nil {
			return err
		}
	}
	level := slog.LevelInfo
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level}))
	if command == "probe" {
		host, port, _ := net.SplitHostPort(c.ListenAddr)
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		probeCtx, cancel := context.WithTimeout(ctx, c.ReadinessTimeout+time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/readyz", nil)
		if err != nil {
			return errors.New("invalid probe address")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return errors.New("readiness probe failed")
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return errors.New("application not ready")
		}
		return nil
	}
	if command == "migrate" {
		direction := "up"
		if len(args) > 1 {
			direction = args[1]
		}
		migrationCtx, cancel := context.WithTimeout(ctx, c.StartupTimeout)
		defer cancel()
		if err := postgres.Migrate(migrationCtx, c.DatabaseURL, os.DirFS("db/migrations"), direction); err != nil {
			return err
		}
		logger.Info("migrations complete", "direction", direction)
		return nil
	}
	startup, cancel := context.WithTimeout(ctx, c.StartupTimeout)
	pool, err := postgres.Open(startup, c.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", c.ListenAddr)
	if err != nil {
		pool.Close()
		return errors.New("HTTP listen failed; check LISTEN_ADDR and port availability")
	}
	q := queries.New(pool)
	server := web.New(c, logger, func(ctx context.Context) error { _, err := q.CheckReady(ctx); return err }, nil)
	logger.Info("service listening")
	err = server.Serve(ctx, ln, func() error { pool.Close(); return nil })
	if err == nil {
		logger.Info("service stopped")
	}
	return err
}
