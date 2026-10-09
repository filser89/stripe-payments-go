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
	before := db.Durable(t)
	_, err = a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: "not-current", ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "open", Evidence: testutil.Evidence(claimed)})
	require.Error(t, err)
	require.Equal(t, before, db.Durable(t), "current-operation guard rejects complete stale result without effects")
	_, err = b.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: "losing-owner", State: "rejected", Evidence: testutil.Evidence(claimed)})
	require.Error(t, err)
	require.Equal(t, before, db.Durable(t))
	control := db.Independent(t)
	_, err = control.Exec(context.Background(), `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`, i.Order.ID)
	require.NoError(t, err)
	before = db.Durable(t)
	_, err = a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "open", Evidence: testutil.Evidence(claimed)})
	require.Error(t, err)
	view, err := a.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "paid", view.Order.Status)
	require.Equal(t, before, db.Durable(t))
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

// DATA-002 DATA-003 LIFE-004 LIFE-006
func TestConcurrentReplacementGuards(t *testing.T) {
	db := testutil.Database(t)
	a := postgres.NewPaymentRepository(db.Pool)
	b := postgres.NewPaymentRepository(db.Independent(t))
	require.NotNil(t, a)
	require.NotNil(t, b)
	db.Relations(t)
	i := testutil.Intent()
	_, err := a.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	claimed := claim(t, a, i, "replacement-predecessor")
	v, err := a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "rejected", Evidence: payment.SessionEvidence{ErrorClass: "validation"}, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: i.Operation.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt, ToState: testutil.Pointer("rejected")}})
	require.NoError(t, err)
	continuation := func() payment.ContinuationIntent {
		op := testutil.Operation(v.Order)
		binding := testutil.Binding(v.Order, op)
		binding.Target = "/api/orders/" + i.Order.ID + "/checkout"
		return payment.ContinuationIntent{OrderID: i.Order.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: v.Operation.Version, Operation: op, Binding: binding, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: op.ID, Kind: "operation_prepared", ObservedAt: i.Order.CreatedAt.Add(time.Second)}}
	}
	x, y := continuation(), continuation()
	before := db.Durable(t)
	control := db.Independent(t)
	// Holding the order row guarantees both committing transactions reach a real DB wait,
	// including implementations that serialize before inserting the new operation.
	release := testutil.HoldOrder(t, control, i.Order.ID)
	defer release()
	type result struct {
		view payment.View
		err  error
		key  string
	}
	done := make(chan result, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { v, e := a.BindContinuation(ctx, x); done <- result{v, e, x.Binding.Key} }()
	go func() { v, e := b.BindContinuation(ctx, y); done <- result{v, e, y.Binding.Key} }()
	testutil.AwaitDatabaseOverlap(t, control)
	release()
	results := []result{}
	for range 2 {
		select {
		case r := <-done:
			results = append(results, r)
		case <-time.After(6 * time.Second):
			t.Fatal("replacement contenders did not join")
		}
	}
	winners := 0
	var winner result
	for _, r := range results {
		if r.err == nil {
			winners++
			winner = r
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 2, db.Counts(t)["payment_operations"])
	require.Equal(t, 2, db.Counts(t)["payment_request_bindings"])
	require.Equal(t, len(before["payment_history"])+1, db.Counts(t)["payment_history"])
	for _, r := range results {
		if r.err != nil {
			_, e := a.LoadBinding(context.Background(), r.key)
			require.Error(t, e, "losing fresh key is unbound")
		}
	}
	current, err := a.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	require.Equal(t, winner.view.Operation.ID, current.Order.CurrentOperationID)
	require.Equal(t, "prepared", current.Operation.State)
	// Every older potentially active operation is a committing-transaction guard.
	nextClaim := payment.Dispatch{OrderID: i.Order.ID, OperationID: current.Operation.ID, ExpectedCurrentOperationID: current.Operation.ID, ExpectedVersion: current.Operation.Version, OwnerToken: "next-owner", Snapshot: current.Operation.Snapshot, FirstDispatchAt: i.Order.CreatedAt, LastDispatchAt: i.Order.CreatedAt}
	nextClaim.Snapshot.FirstDispatchAt = &nextClaim.FirstDispatchAt
	nextClaim.Snapshot.LastDispatchAt = &nextClaim.LastDispatchAt
	nextClaim.Snapshot.ExpiresAt = i.Order.CreatedAt.Add(23*time.Hour + 59*time.Minute).Unix()
	op, err := a.PrepareDispatch(context.Background(), nextClaim)
	require.NoError(t, err)
	current, err = a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: op.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: op.Version, OwnerToken: op.OwnerToken, State: "rejected", Evidence: payment.SessionEvidence{ErrorClass: "validation"}, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: op.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt, ToState: testutil.Pointer("rejected")}})
	require.NoError(t, err)
	z := continuation()
	z.ExpectedCurrentOperationID = current.Operation.ID
	z.ExpectedVersion = current.Operation.Version
	_, err = control.Exec(context.Background(), `UPDATE payment_operations SET state='unresolved', prior_ambiguity=true WHERE id=$1`, i.Operation.ID)
	require.NoError(t, err)
	unsafe := db.Durable(t)
	_, err = b.BindContinuation(context.Background(), z)
	require.Error(t, err)
	require.Equal(t, unsafe, db.Durable(t))
	_, err = control.Exec(context.Background(), `UPDATE payment_operations SET state='rejected', prior_ambiguity=false WHERE id=$1`, i.Operation.ID)
	require.NoError(t, err)
	safe := db.Durable(t)
	_, err = b.BindContinuation(context.Background(), z)
	require.NoError(t, err)
	require.Equal(t, len(safe["payment_operations"])+1, db.Counts(t)["payment_operations"])
}

// DATA-003 LIFE-006
func TestStaleCurrentOperationResultIsAtomic(t *testing.T) {
	for _, tc := range []struct{ name, state string }{{"late_success", "open"}, {"late_rejection", "rejected"}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			a := postgres.NewPaymentRepository(db.Pool)
			b := postgres.NewPaymentRepository(db.Independent(t))
			require.NotNil(t, a)
			require.NotNil(t, b)
			db.Relations(t)
			i := testutil.Intent()
			_, err := a.AcceptInitial(context.Background(), i)
			require.NoError(t, err)
			op := claim(t, a, i, "stale-owner")
			// Model another trusted transaction taking ownership while external work is outstanding.
			_, err = db.Independent(t).Exec(context.Background(), `UPDATE payment_operations SET owner_token='current-owner', version=version+1 WHERE id=$1`, op.ID)
			require.NoError(t, err)
			before := db.Durable(t)
			_, err = b.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: op.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: op.Version, OwnerToken: op.OwnerToken, State: tc.state, Evidence: testutil.Evidence(op), History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: op.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt}})
			require.Error(t, err)
			require.Equal(t, before, db.Durable(t), "unauthorized result cannot mutate current owner or business/history")
		})
	}
}

// LIFE-004 DATA-003
func TestReplacementEligibilityRechecksPaidAtCommit(t *testing.T) {
	db := testutil.Database(t)
	repo := postgres.NewPaymentRepository(db.Pool)
	require.NotNil(t, repo)
	db.Relations(t)
	i := testutil.Intent()
	_, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	op := claim(t, repo, i, "eligible-owner")
	v, err := repo.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: op.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: op.Version, OwnerToken: op.OwnerToken, State: "rejected", Evidence: payment.SessionEvidence{ErrorClass: "validation"}, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: op.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt, ToState: testutil.Pointer("rejected")}})
	require.NoError(t, err)
	next := testutil.Operation(v.Order)
	binding := testutil.Binding(v.Order, next)
	binding.Target = "/api/orders/" + i.Order.ID + "/checkout"
	continuation := payment.ContinuationIntent{OrderID: i.Order.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: v.Operation.Version, Operation: next, Binding: binding, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: next.ID, Kind: "operation_prepared", ObservedAt: i.Order.CreatedAt}}
	control := db.Independent(t)
	tx, err := control.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(context.Background(), `SELECT id FROM payment_orders WHERE id=$1 FOR UPDATE`, i.Order.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	other := postgres.NewPaymentRepository(db.Independent(t))
	require.NotNil(t, other)
	go func() { _, e := other.BindContinuation(ctx, continuation); done <- e }()
	require.Eventually(t, func() bool {
		var n int
		e := control.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&n)
		return e == nil && n >= 1
	}, time.Second, 10*time.Millisecond)
	_, err = tx.Exec(context.Background(), `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`, i.Order.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(context.Background()))
	select {
	case e := <-done:
		require.Error(t, e)
	case <-time.After(5 * time.Second):
		t.Fatal("paid guard contender not joined")
	}
	current, err := repo.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "paid", current.Order.Status)
	require.Equal(t, op.ID, current.Operation.ID)
	require.Equal(t, 1, db.Counts(t)["payment_operations"])
	_, err = repo.LoadBinding(context.Background(), binding.Key)
	require.Error(t, err)
	page, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
	require.NoError(t, err)
	for _, h := range page.Entries {
		require.NotEqual(t, next.ID, h.OperationID, "stale eligibility cannot append replacement history")
	}
}
