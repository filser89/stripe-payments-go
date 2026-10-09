package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres/queries"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type paymentRepository struct{ pool *pgxpool.Pool }

// NewPaymentRepository constructs the transactional, cross-process payment store.
func NewPaymentRepository(pool *pgxpool.Pool) payment.Repository {
	return &paymentRepository{pool: pool}
}
func id(s string) pgtype.UUID { u, e := uuid.Parse(s); return pgtype.UUID{Bytes: u, Valid: e == nil} }
func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC().Truncate(time.Microsecond), Valid: !t.IsZero()}
}
func optionalStamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return stamp(*t)
}
func textValue(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
func stringPointer(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func dbError(err error, order, operation string) error {
	if err == nil {
		return nil
	}
	var known *payment.Error
	if errors.As(err, &known) {
		return err
	}
	code := "temporarily_unavailable"
	if errors.Is(err, pgx.ErrNoRows) {
		code = "not_found"
		order, operation = "", ""
	}
	return &payment.Error{Code: code, OrderID: order, OperationID: operation, Cause: err}
}
func guard(code string, o payment.Order, p payment.Operation) error {
	return &payment.Error{Code: code, OrderID: o.ID, OperationID: p.ID}
}

// Decode only the explicitly selected business columns; internal lease/sequence
// bookkeeping remains private to PostgreSQL.
func decode(data []byte, out any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	names := map[string]string{"id": "ID", "order_id": "OrderID", "operation_id": "OperationID", "current_operation_id": "CurrentOperationID", "payment_status": "Status", "stripe_key": "StripeKey", "stripe_session_id": "SessionID", "stripe_payment_intent_id": "PaymentIntentID", "checkout_url": "CheckoutURL", "request_key": "Key", "stripe_event_id": "EventID", "stripe_request_id": "RequestID", "recorded_at": "ObservedAt"}
	converted := map[string]json.RawMessage{}
	for key, v := range fields {
		if strings.HasSuffix(key, "_at") {
			var at time.Time
			if string(v) != "null" && json.Unmarshal(v, &at) == nil {
				normalized, e := json.Marshal(at.UTC())
				if e != nil {
					return e
				}
				v = normalized
			}
		}
		name, ok := names[key]
		if !ok {
			var parts []string
			for _, part := range strings.Split(key, "_") {
				parts = append(parts, strings.ToUpper(part[:1])+part[1:])
			}
			name = strings.Join(parts, "")
		}
		converted[name] = v
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}
func view(orderData, operationData []byte, expired bool) (payment.View, error) {
	var v payment.View
	if e := decode(orderData, &v.Order); e != nil {
		return v, e
	}
	if e := decode(operationData, &v.Operation); e != nil {
		return v, e
	}
	v.Operation.Snapshot.LastDispatchAt = v.Operation.LastDispatchAt
	v.NeedsInvestigation = v.Operation.InvestigationRequired
	if expired {
		v.Operation.OwnerToken = ""
	}
	return v, nil
}
func (r *paymentRepository) LoadOrder(ctx context.Context, orderID string) (payment.View, error) {
	row, e := queries.New(r.pool).PaymentView(ctx, id(orderID))
	if e != nil {
		return payment.View{}, dbError(e, orderID, "")
	}
	v, e := view(row.OrderData, row.OperationData, row.LeaseExpired)
	return v, dbError(e, orderID, "")
}
func loadBinding(ctx context.Context, q *queries.Queries, key string) (payment.RequestBinding, error) {
	row, e := q.PaymentBinding(ctx, id(key))
	if e != nil {
		return payment.RequestBinding{}, dbError(e, "", "")
	}
	var b payment.RequestBinding
	if e = decode(row.BindingData, &b); e != nil {
		return b, dbError(e, "", "")
	}
	b.View, e = view(row.OrderData, row.OperationData, row.LeaseExpired)
	return b, dbError(e, b.OrderID, b.OperationID)
}
func (r *paymentRepository) LoadBinding(ctx context.Context, key string) (payment.RequestBinding, error) {
	return loadBinding(ctx, queries.New(r.pool), key)
}
func sameBinding(a, b payment.RequestBinding, initial bool) bool {
	equal := a.Method == b.Method && a.Target == b.Target && a.Description == b.Description && a.Amount == b.Amount && a.Currency == b.Currency
	return equal && (initial || (a.OrderID == b.OrderID && a.OperationID == b.OperationID))
}
func snapshotJSON(s payment.Snapshot) ([]byte, error) {
	s.LastDispatchAt = nil
	// Explicit fixed options make the persisted wire contract inspectable and
	// retain the complete request independently of runtime configuration.
	data, e := json.Marshal(s)
	if e != nil {
		return nil, e
	}
	var m map[string]any
	if e = json.Unmarshal(data, &m); e != nil {
		return nil, e
	}
	m["Mode"] = "payment"
	m["UIMode"] = "hosted_page"
	m["PaymentMethodTypes"] = []string{"card"}
	m["Quantity"] = 1
	m["CaptureMethod"] = "automatic"
	m["AutomaticTax"] = false
	m["AllowPromotionCodes"] = false
	m["AdaptivePricing"] = false
	m["AfterExpirationRecovery"] = false
	m["Metadata"] = map[string]string{"order_id": s.OrderID, "operation_id": s.OperationID}
	m["PaymentIntentMetadata"] = m["Metadata"]
	return json.Marshal(m)
}
func insertOperation(ctx context.Context, q *queries.Queries, o payment.Order, p payment.Operation) error {
	s := p.Snapshot
	if p.OrderID != o.ID || s.OrderID != o.ID || s.OperationID != p.ID || s.StripeKey != p.StripeKey || s.Description != o.Description || s.Amount != o.Amount || s.Currency != o.Currency || p.State != "prepared" || s.FirstDispatchAt != nil || s.ExpiresAt != 0 {
		return guard("idempotency_conflict", o, p)
	}
	data, e := snapshotJSON(s)
	if e != nil {
		return e
	}
	return q.InsertPaymentOperation(ctx, queries.InsertPaymentOperationParams{ID: id(p.ID), OrderID: id(p.OrderID), State: p.State, StripeKey: p.StripeKey, Snapshot: data, Version: p.Version, OwnerToken: p.OwnerToken, PriorAmbiguity: p.PriorAmbiguity, EvidenceSource: p.EvidenceSource, InvestigationRequired: p.InvestigationRequired, CreatedAt: stamp(p.CreatedAt), UpdatedAt: stamp(p.UpdatedAt)})
}
func insertBinding(ctx context.Context, q *queries.Queries, b payment.RequestBinding) error {
	return q.InsertPaymentBinding(ctx, queries.InsertPaymentBindingParams{RequestKey: id(b.Key), Method: b.Method, Target: b.Target, OrderID: id(b.OrderID), OperationID: id(b.OperationID), Description: b.Description, Amount: b.Amount, Currency: b.Currency, CreatedAt: stamp(b.CreatedAt)})
}
func addHistory(ctx context.Context, q *queries.Queries, h payment.HistoryEntry) error {
	seq, e := q.NextPaymentSequence(ctx, id(h.OrderID))
	if e != nil {
		return e
	}
	return q.InsertPaymentHistory(ctx, queries.InsertPaymentHistoryParams{OrderID: id(h.OrderID), Sequence: seq, Kind: h.Kind, RecordedAt: stamp(h.ObservedAt), OperationID: id(h.OperationID), FromState: textValue(h.FromState), ToState: textValue(h.ToState), StripeSessionID: textValue(h.SessionID), StripePaymentIntentID: textValue(h.PaymentIntentID), StripeEventID: textValue(h.EventID), StripeRequestID: textValue(h.RequestID), FailureCode: textValue(h.FailureCode), EventAt: optionalStamp(h.EventAt)})
}
func (r *paymentRepository) AcceptInitial(ctx context.Context, in payment.AcceptedIntent) (payment.View, error) {
	if b, e := r.LoadBinding(ctx, in.Binding.Key); e == nil {
		if sameBinding(b, in.Binding, true) {
			return b.View, nil
		}
		return payment.View{}, &payment.Error{Code: "idempotency_conflict"}
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return payment.View{}, dbError(e, "", "")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := queries.New(tx)
	o := in.Order
	p := in.Operation
	b := in.Binding
	if o.CurrentOperationID != p.ID || b.OrderID != o.ID || b.OperationID != p.ID || b.Description != o.Description || b.Amount != o.Amount || b.Currency != o.Currency || b.Method != "POST" || b.Target != "/api/orders" {
		return payment.View{}, guard("idempotency_conflict", o, p)
	}
	e = q.InsertPaymentOrder(ctx, queries.InsertPaymentOrderParams{ID: id(o.ID), Description: o.Description, Amount: o.Amount, Currency: o.Currency, PaymentStatus: o.Status, CurrentOperationID: id(o.CurrentOperationID), CreatedAt: stamp(o.CreatedAt), UpdatedAt: stamp(o.UpdatedAt)})
	if e == nil {
		e = insertOperation(ctx, q, o, p)
	}
	if e == nil {
		e = insertBinding(ctx, q, b)
	}
	if e == nil {
		e = addHistory(ctx, q, payment.HistoryEntry{OrderID: o.ID, OperationID: p.ID, Kind: "order_created", ObservedAt: o.CreatedAt})
	}
	if e == nil {
		e = addHistory(ctx, q, payment.HistoryEntry{OrderID: o.ID, OperationID: p.ID, Kind: "operation_prepared", ObservedAt: p.CreatedAt, ToState: stringPointer("prepared")})
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		var constraint *pgconn.PgError
		if errors.As(e, &constraint) && constraint.Code == "23505" {
			if rb := tx.Rollback(ctx); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
				return payment.View{}, dbError(rb, "", "")
			}
			winner, we := r.LoadBinding(ctx, b.Key)
			if we == nil {
				if sameBinding(winner, b, true) {
					return winner.View, nil
				}
				return payment.View{}, &payment.Error{Code: "idempotency_conflict"}
			}
		}
		return payment.View{}, dbError(e, "", "")
	}
	return r.LoadOrder(ctx, o.ID)
}
func (r *paymentRepository) ReadHistory(ctx context.Context, orderID string, after int64, limit int) (payment.HistoryPage, error) {
	page := payment.HistoryPage{OrderID: orderID, Entries: []payment.HistoryEntry{}, NextAfter: &after}
	if limit < 1 || limit > 100 || after < 0 {
		return page, &payment.Error{Code: "invalid_request"}
	}
	q := queries.New(r.pool)
	if _, e := q.PaymentOrder(ctx, id(orderID)); e != nil {
		return page, dbError(e, orderID, "")
	}
	rows, e := q.PaymentHistory(ctx, queries.PaymentHistoryParams{OrderID: id(orderID), Sequence: after, Limit: int32(limit)})
	if e != nil {
		return page, dbError(e, orderID, "")
	}
	for _, row := range rows {
		var h payment.HistoryEntry
		if e = decode(row, &h); e != nil {
			return page, dbError(e, orderID, "")
		}
		if h.ToState != nil {
			h.State = *h.ToState
		}
		page.Entries = append(page.Entries, h)
		after = h.Sequence
	}
	return page, nil
}

// immutableSnapshot excludes mutable dispatch timing from request identity.
func immutableSnapshot(s payment.Snapshot) payment.Snapshot { s.LastDispatchAt = nil; return s }
func equalSnapshot(a, b payment.Snapshot) bool {
	return reflect.DeepEqual(immutableSnapshot(a), immutableSnapshot(b))
}
