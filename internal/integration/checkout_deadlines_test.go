package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConnectedCheckoutBudgetsAndShutdown(t *testing.T) { // STR-004 STR-005 STR-006 STR-007 LIFE-009 FND-002 FND-003 FND-004
	for _, tc := range []struct {
		name     string
		attempts int
	}{{"one_wire", 1}, {"two_wires", 2}, {"three_wires", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			s := testutil.NewStripeServer(t)
			s.Status = 500
			s.ErrorBody = `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`
			options := testutil.Options()
			options.MaxAttempts = tc.attempts
			j := journeyWith(t, db, db.Pool, s, options)
			start := time.Now()
			v, status := create(t, j, uuid.NewString())
			require.Equal(t, 202, status)
			require.Len(t, s.Wires(), tc.attempts, "SDK retries and business calls share wire ceiling")
			require.Less(t, time.Since(start), options.RequestTimeout+time.Second)
			id, _ := envelopeIDs(t, v)
			view, err := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, "unresolved", view.Operation.State)
			require.Equal(t, "unpaid", view.Order.Status)
			wires := s.Wires()
			for n := 1; n < len(wires); n++ {
				minimum := 250 * time.Millisecond
				if n == 2 {
					minimum = 500 * time.Millisecond
				}
				require.GreaterOrEqual(t, wires[n].At.Sub(wires[n-1].At), minimum-20*time.Millisecond)
				require.Equal(t, wires[0].Header.Get("Idempotency-Key"), wires[n].Header.Get("Idempotency-Key"))
				require.Equal(t, wires[0].Form, wires[n].Form)
			}
		})
	}
	t.Run("blocked_wire_caller_cancel_joins_and_retains_identity", func(t *testing.T) {
		j := journey(t)
		barrier := testutil.NewBarrier()
		j.Stripe.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		defer barrier.Release()
		key := uuid.NewString()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := j.Service.Create(ctx, createInput(key)); done <- err }()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("wire not admitted")
		}
		binding, err := j.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("payment owned work not joined")
		}
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("SDK peer remained blocked")
		}
		view, err := j.Repo.LoadOrder(context.Background(), binding.OrderID)
		require.NoError(t, err)
		require.NotEqual(t, "rejected", view.Operation.State)
		require.Equal(t, binding.OperationID, view.Operation.ID)
		require.NotEmpty(t, view.Operation.StripeKey)
		require.NotNil(t, view.Operation.FirstDispatchAt)
	})
}

func createInput(key string) payment.CreateInput {
	return payment.CreateInput{Description: "Single café product", Amount: 2500, RequestKey: key}
}
