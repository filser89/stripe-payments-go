package config_test

import (
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestLoadValidConfiguration(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://app:private@localhost:5432/app?sslmode=disable"}
	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, ":8080", c.ListenAddr)
	require.Equal(t, "info", c.LogLevel)
	require.Equal(t, 5*time.Second, c.StartupTimeout)
	require.Equal(t, time.Second, c.ReadinessTimeout)
	require.Equal(t, 5*time.Second, c.HeaderTimeout)
	require.Equal(t, 11*time.Second, c.ReadTimeout) // CFG-003
	require.Equal(t, 15*time.Second, c.WriteTimeout)
	require.Equal(t, 60*time.Second, c.IdleTimeout)
	require.Equal(t, 10*time.Second, c.ShutdownGrace)
	require.Equal(t, 5*time.Second, c.CleanupTimeout)
	env["LISTEN_ADDR"] = "127.0.0.1:9090"
	env["READINESS_TIMEOUT"] = "250ms"
	c, err = config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9090", c.ListenAddr)
	require.Equal(t, 250*time.Millisecond, c.ReadinessTimeout)
}

func TestLoadRejectsInvalidConfigurationWithoutSecrets(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing_database", "DATABASE_URL", ""},
		{"invalid_database", "DATABASE_URL", "postgres://user:SECRET@%gh"},
		{"wrong_scheme", "DATABASE_URL", "https://user:SECRET@localhost/app"},
		{"missing_host", "DATABASE_URL", "postgres:///app"},
		{"missing_database_name", "DATABASE_URL", "postgres://localhost/"},
		{"bad_port", "LISTEN_ADDR", "localhost:abc"},
		{"zero_port", "LISTEN_ADDR", "localhost:0"},
		{"bad_log_level", "LOG_LEVEL", "SECRET"},
		{"invalid_timeout", "READINESS_TIMEOUT", "SECRET"},
		{"zero_startup", "DB_STARTUP_TIMEOUT", "0s"},
		{"negative_readiness", "READINESS_TIMEOUT", "-1s"},
		{"zero_header", "HTTP_HEADER_TIMEOUT", "0s"},
		{"zero_read", "HTTP_READ_TIMEOUT", "0s"},
		{"zero_write", "HTTP_WRITE_TIMEOUT", "0s"},
		{"zero_idle", "HTTP_IDLE_TIMEOUT", "0s"},
		{"zero_shutdown", "SHUTDOWN_GRACE", "0s"},
		{"zero_cleanup", "CLEANUP_TIMEOUT", "0s"},
		{"short_container_budget", "COMPOSE_STOP_GRACE_PERIOD", "10s"},
		{"overflow_budget", "SHUTDOWN_GRACE", "2562047h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"DATABASE_URL": "postgres://app:SECRET@localhost:5432/app"}
			env[tc.key] = tc.value
			_, err := config.Load(func(k string) string { return env[k] })
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SECRET")
		})
	}
}
