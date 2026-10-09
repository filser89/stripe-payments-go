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
		dispatchBefore := db.Durable(t)
		release := testutil.FailHistory(t, db.Independent(t), false)
		_, err = repo.PrepareDispatch(context.Background(), dispatch)
		require.Error(t, err)
		require.Equal(t, dispatchBefore, db.Durable(t))
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
		before := db.Durable(t)
		release = testutil.FailHistory(t, db.Independent(t), true)
		_, err = repo.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "open", Evidence: e, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: i.Operation.ID, Kind: "operation_state_changed", ObservedAt: now}})
		require.Error(t, err)
		require.Equal(t, before, db.Durable(t))
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
	before := db.Durable(t)
	replay, err := repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	require.Equal(t, i.Order.ID, replay.Order.ID)
	require.Equal(t, before, db.Durable(t))
	changed := i
	changed.Order.Amount++
	changed.Binding.Amount++
	changed.Operation.Snapshot.Amount++
	_, err = repo.AcceptInitial(context.Background(), changed)
	require.Error(t, err)
	require.Equal(t, before, db.Durable(t))
	for _, collision := range []payment.AcceptedIntent{func() payment.AcceptedIntent {
		x := testutil.Intent()
		x.Operation.StripeKey = i.Operation.StripeKey
		x.Operation.Snapshot.StripeKey = i.Operation.StripeKey
		return x
	}(), func() payment.AcceptedIntent { x := testutil.Intent(); x.Operation.ID = i.Operation.ID; return x }()} {
		_, err = repo.AcceptInitial(context.Background(), collision)
		require.Error(t, err)
		require.Equal(t, before, db.Durable(t))
	}
	for _, statement := range []string{
		`UPDATE payment_orders SET amount=0 WHERE id=$1`,
		`UPDATE payment_orders SET amount=-1 WHERE id=$1`,
		`UPDATE payment_orders SET amount=100001 WHERE id=$1`,
		`UPDATE payment_orders SET currency='eur' WHERE id=$1`,
		`UPDATE payment_orders SET currency='USD' WHERE id=$1`,
	} {
		_, err = db.Independent(t).Exec(context.Background(), statement, i.Order.ID)
		require.Error(t, err, "actual invalid money/currency must be rejected")
		require.Equal(t, before, db.Durable(t))
	}
	// Normal repository paths cannot rewrite accepted purchase/binding/snapshot values.
	for _, mutate := range []func(*payment.AcceptedIntent){
		func(x *payment.AcceptedIntent) {
			x.Order.Description = "rewritten"
			x.Binding.Description = "rewritten"
			x.Operation.Snapshot.Description = "rewritten"
		},
		func(x *payment.AcceptedIntent) { x.Binding.Target = "/api/orders/" + i.Order.ID + "/checkout" },
		func(x *payment.AcceptedIntent) { x.Operation.Snapshot.SuccessURL = "http://localhost:9090/" },
		func(x *payment.AcceptedIntent) { x.Operation.Snapshot.APIVersion = "other" },
	} {
		changed := i
		mutate(&changed)
		_, _ = repo.AcceptInitial(context.Background(), changed)
		require.Equal(t, before, db.Durable(t), "accepted data immutable through repository paths")
	}
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
	before := db.Durable(t)
	_, err = repo.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	_, err = repo.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	require.Equal(t, before, db.Durable(t))
	again, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
	require.NoError(t, err)
	require.Equal(t, append(first.Entries, next.Entries...), again.Entries)
}

// DATA-001 STR-002
func TestPaymentTransactionPhaseMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		commit      bool
	}{
		{"open_write", "open", false}, {"open_commit", "open", true},
		{"rejected_write", "rejected", false}, {"rejected_commit", "rejected", true},
		{"unresolved_write", "unresolved", false}, {"unresolved_commit", "unresolved", true},
		{"expired_write", "expired", false}, {"expired_commit", "expired", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			repo := postgres.NewPaymentRepository(db.Pool)
			require.NotNil(t, repo)
			db.Relations(t)
			i := testutil.Intent()
			_, err := repo.AcceptInitial(context.Background(), i)
			require.NoError(t, err)
			claimed := claim(t, repo, i, "matrix-owner")
			e := testutil.Evidence(claimed)
			if tc.state == "unresolved" {
				// Trusted prior open fixture makes unresolved an actual business transition.
				_, err = db.Independent(t).Exec(context.Background(), `UPDATE payment_operations SET state='open',stripe_session_id=$2,checkout_url=$3,expires_at=$4 WHERE id=$1`, claimed.ID, e.SessionID, e.URL, time.Unix(e.ExpiresAt, 0).UTC())
				require.NoError(t, err)
				claimed.State = "open"
			}
			if tc.state == "expired" {
				e.Status = "expired"
				e.URL = ""
			}
			if tc.state == "rejected" {
				e = payment.SessionEvidence{ErrorClass: "validation", ErrorCode: "invalid_parameter", ObservedAt: i.Order.CreatedAt}
			}
			if tc.state == "unresolved" {
				e = payment.SessionEvidence{ErrorClass: "server", ObservedAt: i.Order.CreatedAt}
			}
			from := claimed.State
			h := payment.HistoryEntry{OrderID: i.Order.ID, OperationID: i.Operation.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt.Add(time.Second), FromState: &from, ToState: &tc.state, SessionID: testutil.Pointer(e.SessionID), RequestID: testutil.Pointer(e.RequestID)}
			observation := payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: tc.state, Evidence: e, PriorAmbiguity: tc.state == "unresolved", History: h}
			before := db.Durable(t)
			release := testutil.FailHistory(t, db.Independent(t), tc.commit)
			_, err = repo.ApplyObservation(context.Background(), observation)
			require.Error(t, err)
			require.Equal(t, before, db.Durable(t), "failed result transaction preserves every value and audit entry")
			release()
			saved, err := repo.ApplyObservation(context.Background(), observation)
			require.NoError(t, err)
			require.Equal(t, tc.state, saved.Operation.State)
			require.Equal(t, i.Order.ID, saved.Order.ID)
			require.Equal(t, i.Operation.ID, saved.Operation.ID)
			page, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
			require.NoError(t, err)
			transitions := 0
			for _, entry := range page.Entries {
				if entry.Kind == h.Kind && entry.ToState != nil && *entry.ToState == tc.state {
					transitions++
					require.Equal(t, h.OrderID, entry.OrderID)
					require.Equal(t, h.OperationID, entry.OperationID)
					require.Equal(t, h.FromState, entry.FromState)
					require.Equal(t, h.ObservedAt, entry.ObservedAt)
					require.Equal(t, h.RequestID, entry.RequestID)
				}
			}
			if from != tc.state {
				require.Equal(t, 1, transitions)
			} else {
				require.Zero(t, transitions, "unchanged observation has no business transition")
			}
			after := db.Durable(t)
			_, _ = repo.ApplyObservation(context.Background(), observation)
			require.Equal(t, after, db.Durable(t), "same result cannot duplicate transition")
		})
	}
	for _, tc := range []struct {
		name   string
		commit bool
	}{{"dispatch_write", false}, {"dispatch_commit", true}, {"replacement_write", false}, {"replacement_commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			repo := postgres.NewPaymentRepository(db.Pool)
			require.NotNil(t, repo)
			db.Relations(t)
			i := testutil.Intent()
			v, err := repo.AcceptInitial(context.Background(), i)
			require.NoError(t, err)
			var apply func() (payment.View, error)
			if tc.name == "dispatch_write" || tc.name == "dispatch_commit" {
				d := dispatchFor(i, "phase-owner")
				apply = func() (payment.View, error) {
					op, e := repo.PrepareDispatch(context.Background(), d)
					return payment.View{Operation: op}, e
				}
			} else {
				op := claim(t, repo, i, "predecessor-owner")
				e := payment.SessionEvidence{ErrorClass: "validation", ObservedAt: i.Order.CreatedAt}
				v, err = repo.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: op.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: op.Version, OwnerToken: op.OwnerToken, State: "rejected", Evidence: e, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: op.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt, FromState: testutil.Pointer("unresolved"), ToState: testutil.Pointer("rejected")}})
				require.NoError(t, err)
				next := testutil.Operation(v.Order)
				b := testutil.Binding(v.Order, next)
				b.Target = "/api/orders/" + i.Order.ID + "/checkout"
				in := payment.ContinuationIntent{OrderID: i.Order.ID, ExpectedCurrentOperationID: op.ID, ExpectedVersion: v.Operation.Version, Operation: next, Binding: b, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: next.ID, Kind: "operation_prepared", ObservedAt: i.Order.CreatedAt.Add(time.Second)}}
				apply = func() (payment.View, error) { return repo.BindContinuation(context.Background(), in) }
			}
			before := db.Durable(t)
			release := testutil.FailHistory(t, db.Independent(t), tc.commit)
			_, err = apply()
			require.Error(t, err)
			require.Equal(t, before, db.Durable(t))
			release()
			_, err = apply()
			require.NoError(t, err)
			after := db.Durable(t)
			_, _ = apply()
			require.Equal(t, after, db.Durable(t), "retry preserves one committed phase and history")
		})
	}
}
func dispatchFor(i payment.AcceptedIntent, owner string) payment.Dispatch {
	now := i.Order.CreatedAt
	s := i.Operation.Snapshot
	s.FirstDispatchAt = &now
	s.LastDispatchAt = &now
	s.ExpiresAt = now.Add(23*time.Hour + 59*time.Minute).Unix()
	return payment.Dispatch{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: i.Operation.Version, OwnerToken: owner, Snapshot: s, FirstDispatchAt: now, LastDispatchAt: now}
}
func claim(t *testing.T, repo payment.Repository, i payment.AcceptedIntent, owner string) payment.Operation {
	t.Helper()
	op, err := repo.PrepareDispatch(context.Background(), dispatchFor(i, owner))
	require.NoError(t, err)
	return op
}

// DATA-002
func TestAssociatedStripeIDConstraintsAndSnapshotImmutability(t *testing.T) {
	db := testutil.Database(t)
	a := postgres.NewPaymentRepository(db.Pool)
	b := postgres.NewPaymentRepository(db.Independent(t))
	require.NotNil(t, a)
	require.NotNil(t, b)
	db.Relations(t)
	i, j := testutil.Intent(), testutil.Intent()
	_, err := a.AcceptInitial(context.Background(), i)
	require.NoError(t, err)
	_, err = b.AcceptInitial(context.Background(), j)
	require.NoError(t, err)
	first := claim(t, a, i, "id-owner")
	e := testutil.Evidence(first)
	e.PaymentIntentID = testutil.Pointer("pi_test_unique")
	_, err = a.ApplyObservation(context.Background(), payment.Observation{OrderID: i.Order.ID, OperationID: i.Operation.ID, ExpectedCurrentOperationID: i.Operation.ID, ExpectedVersion: first.Version, OwnerToken: first.OwnerToken, State: "open", Evidence: e, History: payment.HistoryEntry{OrderID: i.Order.ID, OperationID: i.Operation.ID, Kind: "operation_state_changed", ObservedAt: i.Order.CreatedAt, ToState: testutil.Pointer("open")}})
	require.NoError(t, err)
	second := claim(t, b, j, "second-id-owner")
	secondEvidence := testutil.Evidence(second)
	secondEvidence.PaymentIntentID = testutil.Pointer("pi_test_other_unique")
	require.NotEqual(t, e.SessionID, secondEvidence.SessionID)
	_, err = b.ApplyObservation(context.Background(), payment.Observation{OrderID: j.Order.ID, OperationID: j.Operation.ID, ExpectedCurrentOperationID: j.Operation.ID, ExpectedVersion: second.Version, OwnerToken: second.OwnerToken, State: "open", Evidence: secondEvidence, History: payment.HistoryEntry{OrderID: j.Order.ID, OperationID: j.Operation.ID, Kind: "operation_state_changed", ObservedAt: j.Order.CreatedAt, ToState: testutil.Pointer("open")}})
	require.NoError(t, err)
	positive, err := b.LoadOrder(context.Background(), j.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "open", positive.Operation.State)
	require.Equal(t, secondEvidence.PaymentIntentID, positive.Operation.PaymentIntentID)
	before := db.Durable(t)
	for _, statement := range []string{`UPDATE payment_operations SET stripe_session_id=$1 WHERE id=$2`, `UPDATE payment_operations SET stripe_payment_intent_id=$1 WHERE id=$2`} {
		id := e.SessionID
		if statement == `UPDATE payment_operations SET stripe_payment_intent_id=$1 WHERE id=$2` {
			id = *e.PaymentIntentID
		}
		_, err = db.Independent(t).Exec(context.Background(), statement, id, j.Operation.ID)
		require.Error(t, err)
		require.Equal(t, before, db.Durable(t), "duplicate attached ID is rejected atomically")
	}
	// A normal second dispatch may change last-dispatch bookkeeping, but not its accepted wire snapshot.
	saved, err := a.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	d := dispatchFor(i, "second-owner")
	d.ExpectedVersion = saved.Operation.Version
	d.Snapshot = saved.Operation.Snapshot
	d.Snapshot.Amount++
	d.LastDispatchAt = i.Order.CreatedAt.Add(time.Second)
	d.Snapshot.LastDispatchAt = &d.LastDispatchAt
	_, _ = a.PrepareDispatch(context.Background(), d)
	after, err := a.LoadOrder(context.Background(), i.Order.ID)
	require.NoError(t, err)
	require.Equal(t, testutil.ImmutableSnapshot(saved.Operation.Snapshot), testutil.ImmutableSnapshot(after.Operation.Snapshot))
	require.Equal(t, saved.Order, after.Order)
}

// DATA-001 DATA-004
func TestSameUnresolvedOutcomeAuditAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		identifiers bool
		commit      bool
	}{
		{"server_outcome_write_failure", false, false},   // DATA-001 DATA-004
		{"server_outcome_commit_failure", false, true},   // DATA-001 DATA-004
		{"saved_identifiers_write_failure", true, false}, // DATA-001 DATA-004
		{"saved_identifiers_commit_failure", true, true}, // DATA-001 DATA-004
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			repo := postgres.NewPaymentRepository(db.Pool)
			require.NotNil(t, repo)
			db.Relations(t)
			i := testutil.Intent()
			_, err := repo.AcceptInitial(context.Background(), i)
			require.NoError(t, err)
			claimed := claim(t, repo, i, "same-state-owner")
			require.Equal(t, "unresolved", claimed.State)
			at := i.Order.CreatedAt.Add(time.Second)
			failure := "server"
			e := payment.SessionEvidence{ErrorClass: failure, RequestID: "req_same_state_outcome", ObservedAt: at}
			if tc.identifiers {
				e = testutil.Evidence(claimed)
				e.Status, e.PaymentStatus, e.URL = "complete", "paid", ""
				e.PaymentIntentID = testutil.Pointer("pi_test_same_state_outcome")
				e.RequestID, e.ObservedAt = "req_same_state_identifiers", at
				failure = "confirmation_required"
				e.ErrorClass = failure
			}
			h := payment.HistoryEntry{OrderID: i.Order.ID, OperationID: claimed.ID, Kind: "operation_state_changed", ObservedAt: at, FromState: testutil.Pointer("unresolved"), ToState: testutil.Pointer("unresolved"), RequestID: testutil.Pointer(e.RequestID), FailureCode: &failure}
			if tc.identifiers {
				h.SessionID, h.PaymentIntentID = testutil.Pointer(e.SessionID), e.PaymentIntentID
			}
			observation := payment.Observation{OrderID: i.Order.ID, OperationID: claimed.ID, ExpectedCurrentOperationID: claimed.ID, ExpectedVersion: claimed.Version, OwnerToken: claimed.OwnerToken, State: "unresolved", Evidence: e, PriorAmbiguity: true, History: h}
			before := db.Durable(t)
			pageBefore, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
			require.NoError(t, err)
			release := testutil.FailHistory(t, db.Independent(t), tc.commit)
			_, err = repo.ApplyObservation(context.Background(), observation)
			require.Error(t, err, "a changed external outcome requires an audit insert even when state stays unresolved")
			require.Equal(t, before, db.Durable(t), "failed audit write/commit cannot persist external outcome or identifiers")
			release()
			saved, err := repo.ApplyObservation(context.Background(), observation)
			require.NoError(t, err)
			require.Equal(t, "unpaid", saved.Order.Status)
			require.Equal(t, "unresolved", saved.Operation.State)
			require.Equal(t, &failure, saved.Operation.FailureCode)
			require.True(t, saved.Operation.InvestigationRequired)
			if tc.identifiers {
				require.Equal(t, testutil.Pointer(e.SessionID), saved.Operation.SessionID)
				require.Equal(t, e.PaymentIntentID, saved.Operation.PaymentIntentID)
			} else {
				require.Nil(t, saved.Operation.SessionID)
				require.Nil(t, saved.Operation.PaymentIntentID)
			}
			page, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
			require.NoError(t, err)
			require.Len(t, page.Entries, len(pageBefore.Entries)+1)
			require.Equal(t, pageBefore.Entries, page.Entries[:len(pageBefore.Entries)], "prior audit entries remain append-only")
			entry := page.Entries[len(page.Entries)-1]
			require.Greater(t, entry.Sequence, pageBefore.Entries[len(pageBefore.Entries)-1].Sequence)
			require.Equal(t, h.Kind, entry.Kind)
			require.Equal(t, h.OrderID, entry.OrderID)
			require.Equal(t, h.OperationID, entry.OperationID)
			require.Equal(t, h.FromState, entry.FromState)
			require.Equal(t, h.ToState, entry.ToState)
			require.Equal(t, h.ObservedAt, entry.ObservedAt)
			require.Equal(t, h.RequestID, entry.RequestID)
			require.Equal(t, h.FailureCode, entry.FailureCode)
			require.Equal(t, h.SessionID, entry.SessionID)
			require.Equal(t, h.PaymentIntentID, entry.PaymentIntentID)
			// Repeat through a valid current guard, rather than merely testing stale-owner rejection.
			observation.ExpectedVersion = saved.Operation.Version
			observation.OwnerToken = saved.Operation.OwnerToken
			_, err = repo.ApplyObservation(context.Background(), observation)
			require.NoError(t, err)
			unchanged, err := repo.ReadHistory(context.Background(), i.Order.ID, 0, 100)
			require.NoError(t, err)
			require.Equal(t, page.Entries, unchanged.Entries, "identical business outcome has no duplicate audit transition")
		})
	}
}
