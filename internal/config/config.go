// Package config validates environment settings without exposing their values.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL, ListenAddr, LogLevel                                                       string
	BasicAuthUsername, BasicAuthPassword                                                    string
	StartupTimeout, ReadinessTimeout, HeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout time.Duration
	ShutdownGrace, CleanupTimeout, ContainerStopGrace                                       time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if s := getenv(key); s != "" {
			return s
		}
		return fallback
	}
	c := Config{DatabaseURL: getenv("DATABASE_URL"), ListenAddr: value("LISTEN_ADDR", ":8080"), LogLevel: value("LOG_LEVEL", "info")}
	c.BasicAuthUsername = getenv("BASIC_AUTH_USERNAME")
	c.BasicAuthPassword = getenv("BASIC_AUTH_PASSWORD")
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || strings.Trim(u.Path, "/") == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with host and database")
	}
	_, port, err := net.SplitHostPort(c.ListenAddr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("LISTEN_ADDR must contain a host and port from 1 to 65535")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	for _, field := range []struct {
		key, fallback string
		dst           *time.Duration
	}{
		{"DB_STARTUP_TIMEOUT", "5s", &c.StartupTimeout}, {"READINESS_TIMEOUT", "1s", &c.ReadinessTimeout},
		{"HTTP_HEADER_TIMEOUT", "5s", &c.HeaderTimeout}, {"HTTP_READ_TIMEOUT", "10s", &c.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", "15s", &c.WriteTimeout}, {"HTTP_IDLE_TIMEOUT", "60s", &c.IdleTimeout},
		{"SHUTDOWN_GRACE", "10s", &c.ShutdownGrace}, {"CLEANUP_TIMEOUT", "5s", &c.CleanupTimeout},
		{"COMPOSE_STOP_GRACE_PERIOD", "20s", &c.ContainerStopGrace},
	} {
		d, err := time.ParseDuration(value(field.key, field.fallback))
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive duration", field.key)
		}
		*field.dst = d
	}
	// Subtraction avoids overflowing when a supplied duration approaches MaxInt64.
	if c.ContainerStopGrace <= 5*time.Second || c.ShutdownGrace > c.ContainerStopGrace-5*time.Second || c.CleanupTimeout > c.ContainerStopGrace-5*time.Second-c.ShutdownGrace {
		return Config{}, fmt.Errorf("COMPOSE_STOP_GRACE_PERIOD must allow shutdown grace, cleanup, and 5s margin")
	}
	return c, nil
}
