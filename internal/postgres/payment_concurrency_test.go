package postgres_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestDATA002IndependentConnections(t *testing.T) { // DATA-002
	for _, tc := range []struct {
		name    string
		changed bool
	}{{"same_key_equal", false}, {"same_key_changed", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			a := postgres.NewPaymentRepository(db.Pool)
			b := postgres.NewPaymentRepository(db.Independent(t))
			require.NotNil(t, a, "missing repository A")
			require.NotNil(t, b, "missing repository B")
			db.Relations(t)
			i := testutil.Intent()
			j := testutil.Intent()
			j.Order.Description = i.Order.Description
			j.Order.Amount = i.Order.Amount
			j.Operation.Snapshot.Description = i.Order.Description
			j.Operation.Snapshot.Amount = i.Order.Amount
			j.Binding.Description = i.Order.Description
			j.Binding.Amount = i.Order.Amount
			j.Binding.Key = i.Binding.Key
			if tc.changed {
				j.Order.Amount++
				j.Operation.Snapshot.Amount++
				j.Binding.Amount++
			}
			control := db.Independent(t)
			release := testutil.HoldAcceptance(t, control)
			defer release()
			start := make(chan struct{})
			type result struct {
				view payment.View
				err  error
			}
			done := make(chan result, 2)
			go func() { <-start; v, e := a.AcceptInitial(context.Background(), i); done <- result{v, e} }()
			go func() { <-start; v, e := b.AcceptInitial(context.Background(), j); done <- result{v, e} }()
			close(start)
			testutil.AwaitDatabaseOverlap(t, control)
			release()
			var results []result
			for range 2 {
				select {
				case v := <-done:
					results = append(results, v)
				case <-time.After(5 * time.Second):
					t.Fatal("acceptance did not join")
				}
			}
			if tc.changed {
				require.NotEqual(t, results[0].err == nil, results[1].err == nil)
				for _, r := range results {
					if r.err != nil {
						var conflict *payment.Error
						require.ErrorAs(t, r.err, &conflict)
						require.Equal(t, "idempotency_conflict", conflict.Code)
					}
				}
			} else {
				require.NoError(t, results[0].err)
				require.NoError(t, results[1].err)
				require.Equal(t, results[0].view.Order.ID, results[1].view.Order.ID)
			}
			counts := db.Counts(t)
			require.Equal(t, 1, counts["payment_orders"])
			require.Equal(t, 1, counts["payment_operations"])
			require.Equal(t, 1, counts["payment_request_bindings"])
			require.Equal(t, 2, counts["payment_history"])
		})
	}
}
func TestDATA003IndependentConnections(t *testing.T) { // DATA-003 LIFE-006
	db := testutil.Database(t)
	a := postgres.NewPaymentRepository(db.Pool)
	b := postgres.NewPaymentRepository(db.Independent(t))
	require.NotNil(t, a, "missing durable dispatch coordination")
	require.NotNil(t, b)
	db.Relations(t)
	i := testutil.Intent()
	_, err := a.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	now := i.Order.CreatedAt
	i.Operation.Snapshot.FirstDispatchAt = &now
	i.Operation.Snapshot.LastDispatchAt = &now
	i.Operation.Snapshot.ExpiresAt = now.Add(23*time.Hour + 59*time.Minute).Unix()
	d := payment.Dispatch{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: 1, OwnerToken: "first-owner", Snapshot: i.Operation.Snapshot, FirstDispatchAt: now, LastDispatchAt: now}
	claimed, err := a.PrepareDispatch(context.Background(), d)
	require.NoError(t, err)
	d.OwnerToken = "losing-owner"
	_, err = b.PrepareDispatch(context.Background(), d)
	require.Error(t, err)
	before := db.Counts(t)
	_, err = b.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: "losing-owner", State: "rejected", Evidence: testutil.Evidence(claimed)})
	require.Error(t, err)
	require.Equal(t, before, db.Counts(t))
	control := db.Independent(t)
	_, err = control.Exec(context.Background(), `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`, i.Order.ID)
	require.NoError(t, err)
	_, err = a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "open", Evidence: testutil.Evidence(claimed)})
	require.Error(t, err)
	view, err := a.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "paid", view.Order.Status)
	require.Equal(t, before, db.Counts(t))
}
func TestDATA005IndependentConnections(t *testing.T) { // DATA-005
	db := testutil.Database(t)
	repo := postgres.NewPaymentRepository(db.Pool)
	require.NotNil(t, repo, "missing network-free persistence boundaries")
	db.Relations(t)
	i := testutil.Intent()
	_, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	now := i.Order.CreatedAt
	i.Operation.Snapshot.FirstDispatchAt = &now
	i.Operation.Snapshot.LastDispatchAt = &now
	i.Operation.Snapshot.ExpiresAt = now.Add(23*time.Hour + 59*time.Minute).Unix()
	_, err = repo.PrepareDispatch(context.Background(), payment.Dispatch{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: 1, OwnerToken: "network-owner", Snapshot: i.Operation.Snapshot, FirstDispatchAt: now, LastDispatchAt: now})
	require.NoError(t, err)
	control := db.Independent(t)
	testutil.NoIdleTransaction(t, control)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tx, err := control.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT id FROM payment_orders WHERE id=$1 FOR UPDATE NOWAIT`, i.Order.ID)
	require.NoError(t, err, "dispatch boundary must release locks before external calls")
	require.NoError(t, tx.Rollback(ctx))
}
