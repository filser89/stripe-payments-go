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

func TestDATA001PaymentPersistence(t *testing.T) { // DATA-001
	for _, tc := range []struct {
		name   string
		commit bool
	}{{"atomic_acceptance", false}, {"rollback_between_state_and_history", false}, {"failure_at_commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			repo := postgres.NewPaymentRepository(db.Pool)
			require.NotNil(t, repo, "missing transactional payment repository")
			db.Relations(t)
			control := db.Independent(t)
			intent := testutil.Intent()
			if tc.name != "atomic_acceptance" {
				release := testutil.FailHistory(t, control, tc.commit)
				_, err := repo.AcceptInitial(context.Background(), intent)
				require.Error(t, err)
				require.Equal(t, map[string]int{"payment_orders": 0, "payment_operations": 0, "payment_request_bindings": 0, "payment_history": 0}, db.Counts(t))
				release()
			}
			view, err := repo.AcceptInitial(context.Background(), intent)
			require.NoError(t, err)
			require.Equal(t, intent.Order.ID, view.Order.ID)
			binding, err := repo.LoadBinding(context.Background(), intent.Binding.Key)
			require.NoError(t, err)
			require.Equal(t, intent.Operation.ID, binding.OperationID)
			var orderID string
			require.NoError(t, control.QueryRow(context.Background(), `SELECT id FROM payment_orders WHERE id=$1`, intent.Order.ID).Scan(&orderID))
			require.Equal(t, intent.Order.ID, orderID)
			page, err := repo.ReadHistory(context.Background(), orderID, 0, 100)
			require.NoError(t, err)
			require.Len(t, page.Entries, 2)
			require.Equal(t, "order_created", page.Entries[0].Kind)
			require.Equal(t, "operation_prepared", page.Entries[1].Kind)
			require.Less(t, page.Entries[0].Sequence, page.Entries[1].Sequence)
		})
	}
	t.Run("atomic_dispatch_and_observations", func(t *testing.T) {
		db := testutil.Database(t)
		repo := postgres.NewPaymentRepository(db.Pool)
		require.NotNil(t, repo, "missing dispatch/result transactions")
		db.Relations(t)
		i := testutil.Intent()
		_, err := repo.AcceptInitial(context.Background(), i)
		require.NoError(t, err)
		now := i.Order.CreatedAt
		snapshot := i.Operation.Snapshot
		snapshot.FirstDispatchAt = &now
		snapshot.LastDispatchAt = &now
		snapshot.ExpiresAt = now.Add(23*time.Hour + 59*time.Minute).Unix()
		dispatch := payment.Dispatch{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: 1, OwnerToken: "dispatch-owner", Snapshot: snapshot, FirstDispatchAt: now, LastDispatchAt: now}
		release := testutil.FailHistory(t, db.Independent(t), false)
		_, err = repo.PrepareDispatch(context.Background(), dispatch)
		require.Error(t, err)
		view, err := repo.LoadOrder(context.Background(), i.Order.ID)
		require.NoError(t, err)
		require.Nil(t, view.Operation.FirstDispatchAt)
		require.Equal(t, "prepared", view.Operation.State)
		release()
		claimed, err := repo.PrepareDispatch(context.Background(), dispatch)
		require.NoError(t, err)
		require.Equal(t, &now, claimed.FirstDispatchAt)
		require.Equal(t, snapshot.ExpiresAt, claimed.Snapshot.ExpiresAt)
		require.NotEmpty(t, claimed.OwnerToken)
		e := testutil.Evidence(claimed)
		before := db.Counts(t)
		release = testutil.FailHistory(t, db.Independent(t), true)
		_, err = repo.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "open", Evidence: e, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: i.Operation.ID, Kind: "operation_state_changed", ObservedAt: now}})
		require.Error(t, err)
		require.Equal(t, before, db.Counts(t))
		view, err = repo.LoadOrder(context.Background(), i.Order.ID)
		require.NoError(t, err)
		require.Nil(t, view.Operation.SessionID)
		release()
	})
}
func TestDATA002PaymentPersistence(t *testing.T) { // DATA-002
	db := testutil.Database(t)
	repo := postgres.NewPaymentRepository(db.Pool)
	require.NotNil(t, repo, "missing durable uniqueness")
	db.Relations(t)
	i := testutil.Intent()
	_, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	before := db.Counts(t)
	replay, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	require.Equal(t, i.Order.ID, replay.Order.ID)
	require.Equal(t, before, db.Counts(t))
	changed := i
	changed.Order.Amount++
	changed.Binding.Amount++
	changed.Operation.Snapshot.Amount++
	_, err = repo.AcceptInitial(context.Background(), changed)
	require.Error(t, err)
	require.Equal(t, before, db.Counts(t))
	for _, collision := range []payment.AcceptedIntent{func() payment.AcceptedIntent {
		x := testutil.Intent()
		x.Operation.StripeKey = i.Operation.StripeKey
		x.Operation.Snapshot.StripeKey = i.Operation.StripeKey
		return x
	}(), func() payment.AcceptedIntent { x := testutil.Intent(); x.Operation.ID = i.Operation.ID; return x }()} {
		_, err = repo.AcceptInitial(context.Background(), collision)
		require.Error(t, err)
		require.Equal(t, before, db.Counts(t))
	}
	var invalid bool
	require.NoError(t, db.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='payment_orders'::regclass AND contype='c')`).Scan(&invalid))
	require.True(t, invalid, "money/currency consistency needs database constraints")
}
func TestDATA004PaymentPersistence(t *testing.T) { // DATA-004 HTTP-006
	db := testutil.Database(t)
	repo := postgres.NewPaymentRepository(db.Pool)
	require.NotNil(t, repo, "missing append-only payment history")
	db.Relations(t)
	i := testutil.Intent()
	_, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	other := testutil.Intent()
	_, err = repo.AcceptInitial(context.Background(), other)
	require.NoError(t, err)
	first, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 1)
	require.NoError(t, err)
	require.Len(t, first.Entries, 1)
	require.Equal(t, i.Order.ID, first.Entries[0].OrderID)
	require.Equal(t, first.Entries[0].Sequence, *first.NextAfter)
	next, err := repo.ReadHistory(context.Background(), i.Order.ID, *first.NextAfter, 100)
	require.NoError(t, err)
	require.Len(t, next.Entries, 1)
	require.Greater(t, next.Entries[0].Sequence, first.Entries[0].Sequence)
	empty, err := repo.ReadHistory(context.Background(), i.Order.ID, *next.NextAfter, 50)
	require.NoError(t, err)
	require.Empty(t, empty.Entries)
	require.Equal(t, *next.NextAfter, *empty.NextAfter)
	before := db.Counts(t)
	_, err = repo.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	_, err = repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	require.Equal(t, before, db.Counts(t))
	again, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
	require.NoError(t, err)
	require.Equal(t, append(first.Entries, next.Entries...), again.Entries)
}
