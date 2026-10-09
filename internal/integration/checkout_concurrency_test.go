package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConnectedCheckoutConcurrency(t *testing.T) { // HTTP-003 ID-001 ID-002 LIFE-004 LIFE-006 DATA-002 DATA-003 DATA-005 FND-004
	t.Run("independent_servers_identical_key_while_wire_outstanding", func(t *testing.T) {
		j := journey(t)
		other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		barrier := testutil.NewBarrier()
		j.Stripe.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		defer barrier.Release()
		key := uuid.NewString()
		type result struct {
			v      map[string]any
			status int
		}
		done := make(chan result, 1)
		go func() { v, status := create(t, j, key); done <- result{v, status} }()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("first real wire did not arrive")
		}
		testutil.NoIdleTransaction(t, j.DB.Independent(t))
		binding, err := other.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		require.NotEmpty(t, binding.OperationID)
		counts := j.DB.Counts(t)
		require.Equal(t, 1, counts["payment_orders"])
		require.Equal(t, 1, counts["payment_operations"])
		v, status := create(t, other, key)
		require.Equal(t, 202, status)
		id, op := envelopeIDs(t, v)
		require.Equal(t, binding.OrderID, id)
		require.Equal(t, binding.OperationID, op)
		require.Len(t, j.Stripe.Wires(), 1)
		conflict := testutil.Response(t, other.Handler, testutil.Request("POST", "/api/orders", purchaseBody(key, "changed", 2500), true))
		require.Equal(t, 409, conflict.Code)
		require.Equal(t, counts, j.DB.Counts(t))
		barrier.Release()
		select {
		case res := <-done:
			require.Equal(t, 201, res.status)
			id2, op2 := envelopeIDs(t, res.v)
			require.Equal(t, id, id2)
			require.Equal(t, op, op2)
		case <-time.After(3 * time.Second):
			t.Fatal("first checkout work did not join")
		}
		require.Equal(t, 1, j.Stripe.LogicalObjects())
	})
	t.Run("independent_success_while_other_network_blocked", func(t *testing.T) {
		j := journey(t)
		other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		blockedKey := ""
		barrier := testutil.NewBarrier()
		j.Stripe.Before = func(ctx context.Context, w testutil.Wire) {
			if w.Header.Get("Idempotency-Key") == blockedKey {
				_ = barrier.Wait(ctx)
			}
		}
		first := testutil.Intent()
		_, err := j.Repo.AcceptInitial(context.Background(), first)
		require.NoError(t, err)
		blockedKey = first.Operation.StripeKey
		done := make(chan error, 1)
		go func() { _, e := j.Service.Continue(context.Background(), first.Order.ID, uuid.NewString()); done <- e }()
		defer barrier.Release()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("first operation not held")
		}
		v, status := create(t, other, uuid.NewString())
		require.Equal(t, 201, status)
		id, _ := envelopeIDs(t, v)
		require.NotEqual(t, first.Order.ID, id)
		testutil.NoIdleTransaction(t, j.DB.Independent(t))
		barrier.Release()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("blocked operation did not join")
		}
	})
}
