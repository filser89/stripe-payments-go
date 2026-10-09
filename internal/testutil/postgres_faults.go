package testutil

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// FailHistory injects a real PostgreSQL write or deferred-commit failure. The
// test uses a separate control pool; no production fault hook is introduced.
func FailHistory(t *testing.T, control *pgxpool.Pool, atCommit bool) func() {
	t.Helper()
	ctx := context.Background()
	_, err := control.Exec(ctx, `CREATE FUNCTION checkout_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test-only history fault'; END $$`)
	require.NoError(t, err)
	sql := `CREATE TRIGGER checkout_test_fault BEFORE INSERT ON payment_history FOR EACH ROW EXECUTE FUNCTION checkout_test_fail()`
	if atCommit {
		sql = `CREATE CONSTRAINT TRIGGER checkout_test_fault AFTER INSERT ON payment_history DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION checkout_test_fail()`
	}
	_, err = control.Exec(ctx, sql)
	require.NoError(t, err)
	removed := false
	release := func() {
		if removed {
			return
		}
		removed = true
		_, e := control.Exec(ctx, `DROP TRIGGER checkout_test_fault ON payment_history; DROP FUNCTION checkout_test_fail()`)
		require.NoError(t, e)
	}
	t.Cleanup(release)
	return release
}
func HoldOrder(t *testing.T, control *pgxpool.Pool, id string) func() {
	t.Helper()
	tx, err := control.Begin(context.Background())
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), `SELECT id FROM payment_orders WHERE id=$1 FOR UPDATE`, id)
	require.NoError(t, err)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		require.NoError(t, tx.Rollback(context.Background()))
	}
	t.Cleanup(release)
	return release
}
func NoIdleTransaction(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	var n int
	require.NoError(t, p.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction' AND pid<>pg_backend_pid()`).Scan(&n))
	require.Zero(t, n, "network waits must not hold a business transaction")
}

// HoldAcceptance blocks real INSERT transactions at a database lock. The two
// contenders must both be observed waiting before release; launch is not proof.
func HoldAcceptance(t *testing.T, control *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()
	conn, err := control.Acquire(ctx)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `SELECT pg_advisory_lock(22002002)`)
	require.NoError(t, err)
	_, err = control.Exec(ctx, `CREATE FUNCTION checkout_test_accept_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(22002002); RETURN NEW; END $$; CREATE TRIGGER checkout_test_accept_barrier BEFORE INSERT ON payment_orders FOR EACH ROW EXECUTE FUNCTION checkout_test_accept_barrier()`)
	require.NoError(t, err)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_, e := conn.Exec(ctx, `SELECT pg_advisory_unlock(22002002)`)
		require.NoError(t, e)
		conn.Release()
	}
	t.Cleanup(func() {
		release()
		_, e := control.Exec(ctx, `DROP TRIGGER checkout_test_accept_barrier ON payment_orders; DROP FUNCTION checkout_test_accept_barrier()`)
		require.NoError(t, e)
	})
	return release
}
func AwaitDatabaseOverlap(t *testing.T, control *pgxpool.Pool) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int
		e := control.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&n)
		return e == nil && n >= 2
	}, 3*time.Second, 10*time.Millisecond, "both independent transactions must overlap at real database locks")
}
