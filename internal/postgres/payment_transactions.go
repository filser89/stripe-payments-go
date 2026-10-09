package postgres

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres/queries"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type lockedPayment struct {
	order      payment.Order
	operations []payment.Operation
	expired    map[string]bool
}

func lockPayment(ctx context.Context, q *queries.Queries, orderID string) (lockedPayment, error) {
	var l lockedPayment
	l.expired = map[string]bool{}
	data, e := q.LockPaymentOrder(ctx, id(orderID))
	if e != nil {
		return l, e
	}
	if e = decode(data, &l.order); e != nil {
		return l, e
	}
	rows, e := q.LockPaymentOperations(ctx, id(orderID))
	if e != nil {
		return l, e
	}
	for _, row := range rows {
		var p payment.Operation
		if e = decode(row.Data, &p); e != nil {
			return l, e
		}
		p.Snapshot.LastDispatchAt = p.LastDispatchAt
		l.operations = append(l.operations, p)
		l.expired[p.ID] = row.LeaseExpired
	}
	return l, nil
}
func (l lockedPayment) operation(operationID string) (payment.Operation, error) {
	for _, p := range l.operations {
		if p.ID == operationID {
			return p, nil
		}
	}
	return payment.Operation{}, pgx.ErrNoRows
}
func safePrior(p payment.Operation) bool {
	return p.State == "expired" || p.State == "rejected" && !p.PriorAmbiguity
}
func (l lockedPayment) allPriorSafe(exclude string) bool {
	for _, p := range l.operations {
		if p.ID != exclude && !safePrior(p) {
			return false
		}
	}
	return true
}
func currentGuard(l lockedPayment, p payment.Operation, current string, version int64) bool {
	return l.order.Status != "paid" && l.order.CurrentOperationID == current && p.ID == current && p.Version == version
}
func transactionView(ctx context.Context, q *queries.Queries, order string) (payment.View, error) {
	row, e := q.PaymentView(ctx, id(order))
	if e != nil {
		return payment.View{}, e
	}
	return view(row.OrderData, row.OperationData, row.LeaseExpired)
}
func (r *paymentRepository) BindContinuation(ctx context.Context, in payment.ContinuationIntent) (payment.View, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, "")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	l, e := lockPayment(ctx, q, in.OrderID)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, "")
	}
	p, e := l.operation(l.order.CurrentOperationID)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, "")
	}
	if b, be := loadBinding(ctx, q, in.Binding.Key); be == nil {
		if !sameBinding(b, in.Binding, false) {
			return payment.View{}, guard("idempotency_conflict", l.order, p)
		}
		if e = tx.Commit(ctx); e != nil {
			return payment.View{}, dbError(e, in.OrderID, p.ID)
		}
		return b.View, nil
	} else {
		var pe *payment.Error
		if !errors.As(be, &pe) || pe.Code != "not_found" {
			return payment.View{}, be
		}
	}
	if !currentGuard(l, p, in.ExpectedCurrentOperationID, in.ExpectedVersion) {
		return payment.View{}, guard("checkout_blocked", l.order, p)
	}
	b := in.Binding
	if b.OrderID != l.order.ID || b.OperationID != in.Operation.ID || b.Description != l.order.Description || b.Amount != l.order.Amount || b.Currency != l.order.Currency || b.Target != "/api/orders/"+l.order.ID+"/checkout" || b.Method != "POST" {
		return payment.View{}, guard("idempotency_conflict", l.order, p)
	}
	if in.Operation.ID != p.ID {
		if !l.allPriorSafe("") {
			return payment.View{}, guard("checkout_blocked", l.order, p)
		}
		e = insertOperation(ctx, q, l.order, in.Operation)
		if e == nil {
			e = q.SetCurrentPaymentOperation(ctx, queries.SetCurrentPaymentOperationParams{ID: id(l.order.ID), CurrentOperationID: id(in.Operation.ID), UpdatedAt: stamp(in.Operation.CreatedAt)})
		}
		if e == nil {
			h := in.History
			h.OrderID = l.order.ID
			h.OperationID = in.Operation.ID
			h.Kind = "operation_prepared"
			h.ToState = stringPointer("prepared")
			e = addHistory(ctx, q, h)
		}
	} else if !equalSnapshot(p.Snapshot, in.Operation.Snapshot) {
		return payment.View{}, guard("idempotency_conflict", l.order, p)
	}
	if e == nil {
		e = insertBinding(ctx, q, b)
	}
	var v payment.View
	if e == nil {
		v, e = transactionView(ctx, q, l.order.ID)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	return v, dbError(e, in.OrderID, p.ID)
}
func (r *paymentRepository) PrepareDispatch(ctx context.Context, in payment.Dispatch) (payment.Operation, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return payment.Operation{}, dbError(e, in.OrderID, in.OperationID)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	l, e := lockPayment(ctx, q, in.OrderID)
	if e != nil {
		return payment.Operation{}, dbError(e, in.OrderID, in.OperationID)
	}
	p, e := l.operation(in.OperationID)
	if e != nil {
		return p, dbError(e, in.OrderID, in.OperationID)
	}
	if !currentGuard(l, p, in.ExpectedCurrentOperationID, in.ExpectedVersion) || !l.allPriorSafe(p.ID) || in.OwnerToken == "" || (p.OwnerToken != "" && p.OwnerToken != in.OwnerToken && !l.expired[p.ID]) || p.SessionID != nil || (p.State != "prepared" && p.State != "unresolved") {
		return p, guard("ownership_lost", l.order, p)
	}
	first, last := in.FirstDispatchAt.UTC().Truncate(time.Microsecond), in.LastDispatchAt.UTC().Truncate(time.Microsecond)
	snapshot := in.Snapshot
	snapshot.FirstDispatchAt = &first
	snapshot.LastDispatchAt = nil
	original := p.Snapshot
	original.LastDispatchAt = nil
	if p.FirstDispatchAt == nil {
		original.FirstDispatchAt = &first
		original.ExpiresAt = snapshot.ExpiresAt
	} else if !first.Equal(*p.FirstDispatchAt) {
		return p, guard("ownership_lost", l.order, p)
	}
	if !equalSnapshot(snapshot, original) || last.Before(first) || (p.LastDispatchAt != nil && last.Before(*p.LastDispatchAt)) || snapshot.ExpiresAt != first.Add(23*time.Hour+59*time.Minute).Unix() || last.Sub(first) >= 23*time.Hour {
		return p, guard("checkout_blocked", l.order, p)
	}
	data, e := snapshotJSON(snapshot)
	if e != nil {
		return p, dbError(e, in.OrderID, p.ID)
	}
	e = q.ClaimPaymentDispatch(ctx, queries.ClaimPaymentDispatchParams{ID: id(p.ID), Snapshot: data, OwnerToken: in.OwnerToken, FirstDispatchAt: stamp(first), LastDispatchAt: stamp(last), DispatchExpiresAt: pgtype.Int8{Int64: snapshot.ExpiresAt, Valid: true}})
	if e == nil {
		e = addHistory(ctx, q, payment.HistoryEntry{OrderID: p.OrderID, OperationID: p.ID, Kind: "dispatch_started", ObservedAt: last, ToState: stringPointer("unresolved")})
	}
	var v payment.View
	if e == nil {
		v, e = transactionView(ctx, q, p.OrderID)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	return v.Operation, dbError(e, in.OrderID, p.ID)
}
func (r *paymentRepository) ApplyObservation(ctx context.Context, in payment.Observation) (payment.View, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, in.OperationID)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	l, e := lockPayment(ctx, q, in.OrderID)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, in.OperationID)
	}
	p, e := l.operation(in.OperationID)
	if e != nil {
		return payment.View{}, dbError(e, in.OrderID, in.OperationID)
	}
	if !currentGuard(l, p, in.ExpectedCurrentOperationID, in.ExpectedVersion) || p.OwnerToken != in.OwnerToken {
		return payment.View{}, guard("ownership_lost", l.order, p)
	}
	empty := reflect.DeepEqual(in.Evidence, payment.SessionEvidence{})
	// A matching same-state empty observation is strictly coordination cleanup.
	if in.OwnerToken != "" && in.State == p.State && empty && in.History.Kind == "" && in.History.FailureCode == nil {
		e = q.ReleasePaymentClaim(ctx, id(p.ID))
		var v payment.View
		if e == nil {
			v, e = transactionView(ctx, q, p.OrderID)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		return v, dbError(e, in.OrderID, p.ID)
	}
	updated := p
	updated.State = in.State
	updated.PriorAmbiguity = in.PriorAmbiguity
	updated.OwnerToken = ""
	updated.FailureCode = in.History.FailureCode
	ev := in.Evidence
	mismatch := in.History.FailureCode != nil && *in.History.FailureCode == "evidence_mismatch"
	if mismatch {
		updated.CheckoutURL = nil
		updated.EvidenceSource = "mismatched"
		updated.InvestigationRequired = true
	} else if ev.SessionID != "" {
		if (p.SessionID != nil && *p.SessionID != ev.SessionID) || (p.PaymentIntentID != nil && ev.PaymentIntentID != nil && *p.PaymentIntentID != *ev.PaymentIntentID) {
			return payment.View{}, guard("ownership_lost", l.order, p)
		}
		updated.SessionID = stringPointer(ev.SessionID)
		if ev.PaymentIntentID != nil {
			updated.PaymentIntentID = ev.PaymentIntentID
		}
		updated.CheckoutURL = stringPointer(ev.URL)
		if ev.ExpiresAt != 0 {
			expiry := time.Unix(ev.ExpiresAt, 0).UTC()
			updated.ExpiresAt = &expiry
		}
		updated.EvidenceSource = "stripe"
		updated.InvestigationRequired = false
	} else if ev.ErrorClass != "" {
		updated.EvidenceSource = "stripe"
	}
	if ev.ErrorClass == "server" || ev.ErrorClass == "idempotency" {
		updated.InvestigationRequired = true
	}
	if in.State == "rejected" {
		updated.InvestigationRequired = true
	}
	// Re-reading unchanged evidence neither advances the version nor creates history.
	compare := updated
	compare.OwnerToken = p.OwnerToken
	unchanged := reflect.DeepEqual(compare, p)
	if !unchanged || p.OwnerToken != "" {
		at := ev.ObservedAt
		if at.IsZero() {
			at = in.History.ObservedAt
		}
		if at.IsZero() {
			at = time.Now()
		}
		e = q.SavePaymentObservation(ctx, queries.SavePaymentObservationParams{ID: id(p.ID), State: updated.State, StripeSessionID: textValue(updated.SessionID), StripePaymentIntentID: textValue(updated.PaymentIntentID), CheckoutUrl: textValue(updated.CheckoutURL), ExpiresAt: optionalStamp(updated.ExpiresAt), PriorAmbiguity: updated.PriorAmbiguity, EvidenceSource: updated.EvidenceSource, FailureCode: textValue(updated.FailureCode), InvestigationRequired: updated.InvestigationRequired, LastObservedAt: stamp(at), StripeRequestID: textValue(stringPointer(ev.RequestID))})
		if e == nil && p.State != in.State {
			h := in.History
			h.OrderID = p.OrderID
			h.OperationID = p.ID
			h.Kind = "operation_state_changed"
			if h.ObservedAt.IsZero() {
				h.ObservedAt = at
			}
			if h.FromState == nil {
				h.FromState = &p.State
			}
			h.ToState = &in.State
			e = addHistory(ctx, q, h)
		}
	}
	var v payment.View
	if e == nil {
		v, e = transactionView(ctx, q, p.OrderID)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	return v, dbError(e, in.OrderID, p.ID)
}

// ReleaseDispatch releases only the matching fenced claim, retaining every
// business value, immutable snapshot, lease timestamp and audit entry.
func (r *paymentRepository) ReleaseDispatch(ctx context.Context, in payment.Observation) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return dbError(e, in.OrderID, in.OperationID)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	l, e := lockPayment(ctx, q, in.OrderID)
	if e != nil {
		return dbError(e, in.OrderID, in.OperationID)
	}
	p, e := l.operation(in.OperationID)
	if e != nil {
		return dbError(e, in.OrderID, in.OperationID)
	}
	if !currentGuard(l, p, in.ExpectedCurrentOperationID, in.ExpectedVersion) || p.OwnerToken != in.OwnerToken || in.OwnerToken == "" {
		return guard("ownership_lost", l.order, p)
	}
	e = q.ReleasePaymentClaim(ctx, id(p.ID))
	if e == nil {
		e = tx.Commit(ctx)
	}
	return dbError(e, in.OrderID, in.OperationID)
}
