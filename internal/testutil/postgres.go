package testutil

import (
	"context"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type DB struct {
	URL       string
	Pool      *pgxpool.Pool
	Container *testcontainers.DockerContainer
}

func Database(t *testing.T) *DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := testcontainers.Run(ctx, "postgres:18.6-alpine", testcontainers.WithEnv(map[string]string{"POSTGRES_USER": "checkout", "POSTGRES_PASSWORD": "test-only-fixture", "POSTGRES_DB": "checkout"}), testcontainers.WithExposedPorts("5432/tcp"), testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)))
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	url := fmt.Sprintf("postgres://checkout:test-only-fixture@%s/checkout?sslmode=disable", net.JoinHostPort(host, port.Port()))
	pool, err := postgres.Open(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(source), "../..")
	require.NoError(t, postgres.Migrate(ctx, url, os.DirFS(filepath.Join(root, "db/migrations")), "up"))
	var version string
	require.NoError(t, pool.QueryRow(ctx, "SHOW server_version").Scan(&version))
	require.Contains(t, version, "18.6")
	return &DB{url, pool, c}
}
func (d *DB) Independent(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := postgres.Open(context.Background(), d.URL)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}
func (d *DB) Relations(t *testing.T) {
	t.Helper()
	for _, name := range []string{"payment_orders", "payment_operations", "payment_request_bindings", "payment_history"} {
		var present bool
		require.NoError(t, d.Pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&present))
		require.True(t, present, "application migration must supply "+name)
	}
}
func (d *DB) Counts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, name := range []string{"payment_orders", "payment_operations", "payment_request_bindings", "payment_history"} {
		var n int
		require.NoError(t, d.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+name).Scan(&n))
		out[name] = n
	}
	return out
}

// DurableRows uses independent SQL reads, including every column and prior audit value.
// Ordering by complete row text avoids depending on repository projection or sequence allocation.
func DurableRows(ctx context.Context, control *pgxpool.Pool) (map[string][]string, error) {
	out := map[string][]string{}
	for _, table := range []string{"payment_orders", "payment_operations", "payment_request_bindings", "payment_history"} {
		rows, err := control.Query(ctx, "SELECT row_to_json(t)::text FROM "+table+" t ORDER BY row_to_json(t)::text")
		if err != nil {
			return nil, err
		}
		out[table] = []string{}
		for rows.Next() {
			var row string
			if err = rows.Scan(&row); err != nil {
				rows.Close()
				return nil, err
			}
			out[table] = append(out[table], row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (d *DB) Durable(t *testing.T) map[string][]string {
	t.Helper()
	out, err := DurableRows(context.Background(), d.Independent(t))
	require.NoError(t, err)
	return out
}
