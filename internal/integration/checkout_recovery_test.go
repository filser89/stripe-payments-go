package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConnectedCheckoutRecovery(t *testing.T) { // HTTP-007 ID-001 STR-001 STR-002 STR-003 STR-007 STR-008 REC-001 REC-002 REC-003 REC-004 DATA-001 DATA-004 SEC-002 SEC-003
	t.Run("response_lost_restart_reuses_snapshot", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		j.Stripe.Drop = true
		v, status := create(t, j, key)
		require.Equal(t, 202, status)
		id, op := envelopeIDs(t, v)
		before, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Nil(t, before.Operation.SessionID)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		require.NotNil(t, before.Operation.FirstDispatchAt)
		require.NotNil(t, before.Operation.LastDispatchAt)
		require.Equal(t, before.Operation.FirstDispatchAt.Unix()+int64((23*time.Hour+59*time.Minute)/time.Second), before.Operation.Snapshot.ExpiresAt)
		j.Stripe.Drop = false
		options := j.Options
		options.Origin = "http://127.0.0.1:9090"
		options.MinAmount = 5000
		options.MaxAmount = 6000
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, options)
		v, status = create(t, restart, key)
		require.Equal(t, 200, status)
		id2, op2 := envelopeIDs(t, v)
		require.Equal(t, id, id2)
		require.Equal(t, op, op2)
		after, err := restart.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, before.Operation.StripeKey, after.Operation.StripeKey)
		require.Equal(t, before.Operation.Snapshot, after.Operation.Snapshot)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		for _, wire := range j.Stripe.Wires() {
			if wire.Method == "POST" {
				testutil.CheckWire(t, wire, before.Operation.Snapshot)
			}
		}
	})
	t.Run("acceptance_failure_sends_nothing", func(t *testing.T) {
		j := journey(t)
		release := testutil.FailHistory(t, j.DB.Independent(t), true)
		v, status := create(t, j, uuid.NewString())
		require.Equal(t, 503, status)
		require.Equal(t, "temporarily_unavailable", v["error"].(map[string]any)["code"])
		require.Empty(t, j.Stripe.Wires())
		require.Equal(t, map[string]int{"payment_orders": 0, "payment_operations": 0, "payment_request_bindings": 0, "payment_history": 0}, j.DB.Counts(t))
		release()
	})
	t.Run("success_before_result_commit_failure_then_restart", func(t *testing.T) {
		j := journey(t)
		control := j.DB.Independent(t)
		var release func()
		j.Stripe.Before = func(ctx context.Context, wire testutil.Wire) {
			if wire.Method == "POST" {
				var committed int
				require.NoError(t, control.QueryRow(ctx, `SELECT count(*) FROM payment_operations WHERE first_dispatch_at IS NOT NULL AND last_dispatch_at IS NOT NULL AND stripe_key=$1`, wire.Header.Get("Idempotency-Key")).Scan(&committed))
				require.Equal(t, 1, committed, "dispatch identity visible independently before send completes")
				testutil.NoIdleTransaction(t, control)
				release = testutil.FailHistory(t, control, true)
			}
		}
		key := uuid.NewString()
		v, status := create(t, j, key)
		require.Equal(t, 202, status, "uncommitted SDK result cannot be reported as 201")
		id, op := envelopeIDs(t, v)
		view, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Nil(t, view.Operation.SessionID)
		require.Nil(t, view.Operation.CheckoutURL)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		release()
		j.Stripe.Before = nil
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		v, status = create(t, restart, key)
		require.Equal(t, 200, status)
		id2, op2 := envelopeIDs(t, v)
		require.Equal(t, id, id2)
		require.Equal(t, op, op2)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		saved, err := restart.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, saved.Operation.SessionID)
		require.Equal(t, view.Operation.Snapshot, saved.Operation.Snapshot)
	})
	t.Run("old_creation_refuses_post_at_safe_boundary", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		j.Stripe.Drop = true
		v, status := create(t, j, key)
		require.Equal(t, 202, status)
		id, _ := envelopeIDs(t, v)
		saved, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		options := j.Options
		options.Now = func() time.Time { return saved.Operation.FirstDispatchAt.Add(23 * time.Hour) }
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, options)
		wireBefore := len(j.Stripe.Wires())
		v, status = create(t, restart, key)
		require.Equal(t, 202, status)
		require.Equal(t, wireBefore, len(j.Stripe.Wires()))
		require.True(t, v["operation"].(map[string]any)["investigation_required"].(bool))
		require.False(t, v["can_start_new_attempt"].(bool))
	})
}
