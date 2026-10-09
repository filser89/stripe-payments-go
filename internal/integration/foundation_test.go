package integration_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/postgres/queries"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func database(t *testing.T) (*testcontainers.DockerContainer, string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Keep the published endpoint stable across stop/start, as Compose does.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, hostPort, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	container, err := testcontainers.Run(ctx, "postgres:18.6-alpine",
		testcontainers.WithEnv(map[string]string{"POSTGRES_USER": "foundation", "POSTGRES_PASSWORD": "fixture-only", "POSTGRES_DB": "foundation"}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithHostConfigModifier(func(h *dockercontainer.HostConfig) {
			h.PortBindings = network.PortMap{network.MustParsePort("5432/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: hostPort}}}
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	url := fmt.Sprintf("postgres://foundation:fixture-only@%s/foundation?sslmode=disable", net.JoinHostPort(host, port.Port()))
	pool, err := postgres.Open(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return container, url, pool
}
func TestDatabaseMigrationsReadinessAndRestart(t *testing.T) {
	container, url, pool := database(t)
	ctx := context.Background()
	t.Run("generated_readiness_query", func(t *testing.T) {
		value, err := queries.New(pool).CheckReady(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 1, value)
	})
	t.Run("empty_production_migrations", func(t *testing.T) {
		require.NoError(t, postgres.Migrate(ctx, url, os.DirFS("../../db/migrations"), "up"))
	})
	t.Run("apply_repeat_and_rollback", func(t *testing.T) {
		source := os.DirFS("testdata/migrations")
		require.NoError(t, postgres.Migrate(ctx, url, source, "up"))
		require.NoError(t, postgres.Migrate(ctx, url, source, "up"))
		_, err := pool.Exec(ctx, "INSERT INTO foundation_fixture VALUES (1, 'retained')")
		require.NoError(t, err)
		require.NoError(t, postgres.Migrate(ctx, url, source, "down"))
		var absent bool
		require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('foundation_fixture') IS NULL").Scan(&absent))
		require.True(t, absent)
		require.NoError(t, postgres.Migrate(ctx, url, source, "up"))
		_, err = pool.Exec(ctx, "INSERT INTO foundation_fixture VALUES (1, 'retained')")
		require.NoError(t, err)
	})
	t.Run("failing_transaction_leaves_no_partial_table", func(t *testing.T) {
		require.Error(t, postgres.Migrate(ctx, url, os.DirFS("testdata/failing"), "up"))
		var absent bool
		require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('should_rollback') IS NULL").Scan(&absent))
		require.True(t, absent)
	})
	t.Run("outage_recovery_and_retained_data", func(t *testing.T) {
		c, err := config.Load(func(k string) string {
			if k == "DATABASE_URL" {
				return url
			}
			return ""
		})
		require.NoError(t, err)
		c.ReadinessTimeout = 100 * time.Millisecond
		s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(ctx context.Context) error { _, err := queries.New(pool).CheckReady(ctx); return err }, nil)
		probe := func(path string) int {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			return w.Code
		}
		require.Equal(t, 200, probe("/readyz"))
		timeout := time.Second
		require.NoError(t, container.Stop(ctx, &timeout))
		require.Equal(t, 503, probe("/readyz"))
		require.Equal(t, 200, probe("/healthz"))
		require.NoError(t, container.Start(ctx))
		require.Eventually(t, func() bool {
			return probe("/readyz") == 200
		}, 15*time.Second, 100*time.Millisecond)
		var value string
		require.NoError(t, pool.QueryRow(ctx, "SELECT value FROM foundation_fixture WHERE id=1").Scan(&value))
		require.Equal(t, "retained", value)
		require.NoError(t, postgres.Migrate(ctx, url, os.DirFS("testdata/migrations"), "up"))
		var version int
		require.NoError(t, pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
		require.Equal(t, 1, version)
	})
}
func TestOpenRejectsUnavailableDatabaseWithinDeadline(t *testing.T) {
	// A TCP peer accepts connections but never completes the PostgreSQL handshake.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	pool, err := postgres.Open(ctx, "postgres://app:SECRET@"+ln.Addr().String()+"/app?sslmode=disable")
	if pool != nil {
		pool.Close()
	}
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET")
	require.Less(t, time.Since(start), time.Second)
	select {
	case c := <-accepted:
		_ = c.Close()
	default:
	}
}
func TestShutdownCancelsActivePostgresQuery(t *testing.T) {
	_, url, pool := database(t)
	c, err := config.Load(func(k string) string {
		if k == "DATABASE_URL" {
			return url
		}
		return ""
	})
	require.NoError(t, err)
	c.ShutdownGrace = 50 * time.Millisecond
	c.CleanupTimeout = 2 * time.Second
	finished := make(chan error, 1)
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), pool.Ping, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, e := pool.Exec(r.Context(), "SELECT pg_sleep(30) /* foundation_shutdown */")
		finished <- e
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closed atomic.Bool
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln, func() error { pool.Close(); closed.Store(true); return nil }) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, e := http.Get("http://" + ln.Addr().String() + "/work")
		if e == nil {
			_ = resp.Body.Close()
		}
	}()
	require.Eventually(t, func() bool {
		var active bool
		e := pool.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE query LIKE 'SELECT pg_sleep(30)%' AND state='active')").Scan(&active)
		return e == nil && active
	}, 5*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel database work")
	}
	require.Error(t, <-finished)
	require.True(t, closed.Load())
	<-clientDone
}
