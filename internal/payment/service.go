package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type Service struct {
	repo    Repository
	gateway Gateway
	opts    Options
}
type requestIDKey struct{}
type actionKey struct{}
type responseWindowKey struct{}
type responseWindow struct {
	mu       sync.Mutex
	deadline time.Time
}

func ResponseDeadline(ctx context.Context) time.Time {
	window, _ := ctx.Value(responseWindowKey{}).(*responseWindow)
	if window == nil {
		return time.Time{}
	}
	window.mu.Lock()
	defer window.mu.Unlock()
	return window.deadline
}
func (s *Service) finalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if window, ok := ctx.Value(responseWindowKey{}).(*responseWindow); ok {
		window.mu.Lock()
		if !window.deadline.IsZero() && window.deadline.Before(deadline) {
			deadline = window.deadline
		}
		window.deadline = deadline
		window.mu.Unlock()
	}
	return context.WithDeadline(ctx, deadline)
}

type workDeadlineKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, responseWindowKey{}, &responseWindow{})
	return context.WithValue(ctx, requestIDKey{}, id)
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
func New(r Repository, g Gateway, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Wait == nil {
		o.Wait = wait
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{repo: r, gateway: g, opts: o}
}
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func identity() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
func CanonicalID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || s[14] != '4' || !strings.ContainsRune("89ab", rune(s[19])) {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func ValidDescription(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 200 {
		return false
	}
	r := []rune(s)
	if unicode.IsSpace(r[0]) || unicode.IsSpace(r[len(r)-1]) {
		return false
	}
	for _, c := range r {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func ptr[T any](v T) *T { return &v }
func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func failure(kind string, v View, e error) *Error {
	return &Error{Code: kind, OrderID: v.Order.ID, OperationID: v.Operation.ID, Cause: e}
}
func (s *Service) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = context.WithValue(ctx, workDeadlineKey{}, time.Now().Add(s.opts.RequestTimeout-time.Second))
	return context.WithTimeout(ctx, s.opts.RequestTimeout)
}
func (s *Service) compatible(op Operation) bool {
	return op.Snapshot.SDKVersion == s.opts.SDKVersion && op.Snapshot.APIVersion == s.opts.APIVersion
}
func (s *Service) safe(op Operation) bool {
	return op.FirstDispatchAt != nil && s.opts.Now().Sub(*op.FirstDispatchAt) < 23*time.Hour
}
func (s *Service) view(v View) View {
	op := v.Operation
	active := v.Order.Status != "paid" && op.State != "paid"
	compatible := s.compatible(op)
	mismatch := op.EvidenceSource == "mismatched" || (op.FailureCode != nil && *op.FailureCode == "evidence_mismatch")
	v.CanResume = active && compatible && !mismatch && op.OwnerToken == "" && op.State == "open" && op.CheckoutURL != nil && *op.CheckoutURL != "" && op.ExpiresAt != nil && op.ExpiresAt.After(s.opts.Now())
	v.CanRetrySameOperation = active && compatible && !mismatch && op.OwnerToken == "" && (op.State == "prepared" || op.State == "unresolved" && (op.SessionID != nil || s.safe(op)))
	v.CanStartNewAttempt = active && compatible && !mismatch && op.ID == v.Order.CurrentOperationID && op.OwnerToken == "" && ((op.State == "rejected" && !op.PriorAmbiguity) || (op.State == "expired" && op.EvidenceSource == "stripe" && !mismatch))
	v.NeedsInvestigation = op.InvestigationRequired || mismatch || !compatible || op.State == "rejected" || op.FailureCode != nil && (*op.FailureCode == "server" || *op.FailureCode == "idempotency" || *op.FailureCode == "validation" || *op.FailureCode == "credential" || *op.FailureCode == "permission" || *op.FailureCode == "generic" || *op.FailureCode == "temporarily_unavailable") || op.State == "unresolved" && op.FirstDispatchAt != nil && s.opts.Now().Sub(*op.FirstDispatchAt) >= 15*time.Minute
	if !v.CanResume {
		v.Operation.CheckoutURL = nil
	}
	return v
}
func (s *Service) outcome(v View) Outcome {
	v = s.view(v)
	return Outcome{Order: v.Order, Operation: v.Operation, CanResume: v.CanResume, CanRetrySameOperation: v.CanRetrySameOperation, CanStartNewAttempt: v.CanStartNewAttempt, NeedsInvestigation: v.NeedsInvestigation, Established: v.Operation.State == "open" || v.Operation.State == "expired" || v.Operation.State == "complete_unpaid" && v.Operation.SessionID == nil || v.Order.Status == "paid", Pending: v.Operation.State == "prepared" || v.Operation.State == "unresolved", ConfirmedRejected: v.Operation.State == "rejected" && !v.Operation.PriorAmbiguity}
}
func (s *Service) log(ctx context.Context, action string, out Outcome, err error, kind string) {
	result := "established"
	if out.NewlyAccepted {
		result = "accepted"
	}
	if out.Pending {
		result = "pending"
	}
	if out.ConfirmedRejected {
		result = "rejected"
	}
	if err != nil {
		result = "failed"
		if code(err) == "checkout_blocked" {
			result = "blocked"
		}
		if code(err) == "checkout_rejected" {
			result = "rejected"
		}
	}
	attrs := []any{"action", action, "outcome", result}
	if id := RequestID(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	if out.Order.ID != "" {
		attrs = append(attrs, "order_id", out.Order.ID, "payment_status", out.Order.Status)
	}
	if out.Operation.ID != "" {
		attrs = append(attrs, "operation_id", out.Operation.ID, "operation_state", out.Operation.State)
	}
	if kind == "" && err != nil {
		kind = code(err)
	}
	if kind != "" {
		attrs = append(attrs, "error_kind", kind, "failure_code", kind)
	}
	s.opts.Logger.InfoContext(ctx, "payment outcome", attrs...)
}
func (s *Service) intent(order Order) (Operation, error) {
	id, e := identity()
	if e != nil {
		return Operation{}, e
	}
	key, e := identity()
	if e != nil {
		return Operation{}, e
	}
	at := s.opts.Now().UTC().Truncate(time.Microsecond)
	snap := Snapshot{OrderID: order.ID, OperationID: id, Description: order.Description, Amount: order.Amount, Currency: order.Currency, StripeKey: key, SuccessURL: strings.TrimSuffix(s.opts.Origin, "/") + "/?order_id=" + order.ID + "&checkout_return=success", CancelURL: strings.TrimSuffix(s.opts.Origin, "/") + "/?order_id=" + order.ID + "&checkout_return=cancel", SDKVersion: s.opts.SDKVersion, APIVersion: s.opts.APIVersion}
	return Operation{ID: id, OrderID: order.ID, State: "prepared", StripeKey: key, Snapshot: snap, Version: 1, CreatedAt: at, UpdatedAt: at}, nil
}
func (s *Service) Create(ctx context.Context, in CreateInput) (out Outcome, err error) {
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	ctx = context.WithValue(ctx, actionKey{}, "create")
	kind := ""
	defer func() { s.log(ctx, "create", out, err, kind) }()
	if !CanonicalID(in.RequestKey) || !ValidDescription(in.Description) || in.Amount <= 0 {
		return out, &Error{Code: "invalid_request"}
	}
	b, e := s.repo.LoadBinding(ctx, in.RequestKey)
	if e == nil {
		if b.Method != "POST" || b.Target != "/api/orders" || b.Description != in.Description || b.Amount != in.Amount || b.Currency != s.opts.Currency {
			return out, &Error{Code: "idempotency_conflict"}
		}
		out, kind, err = s.execute(ctx, b.View, &externalBudget{s: s}, true)
		return
	}
	if code(e) != "not_found" {
		kind = "database_read"
		return out, &Error{Code: "temporarily_unavailable", Cause: e}
	}
	if in.Amount < s.opts.MinAmount || in.Amount > s.opts.MaxAmount {
		return out, &Error{Code: "invalid_request"}
	}
	id, e := identity()
	if e != nil {
		return out, &Error{Code: "temporarily_unavailable", Cause: e}
	}
	at := s.opts.Now().UTC().Truncate(time.Microsecond)
	order := Order{ID: id, Description: in.Description, Amount: in.Amount, Currency: s.opts.Currency, Status: "unpaid", CreatedAt: at, UpdatedAt: at}
	op, e := s.intent(order)
	if e != nil {
		return out, &Error{Code: "temporarily_unavailable", Cause: e}
	}
	order.CurrentOperationID = op.ID
	binding := RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: id, OperationID: op.ID, Description: in.Description, Amount: in.Amount, Currency: order.Currency, CreatedAt: at}
	v, e := s.repo.AcceptInitial(ctx, AcceptedIntent{Order: order, Operation: op, Binding: binding, History: HistoryEntry{OrderID: id, OperationID: op.ID, Kind: "order_created", State: "prepared", ObservedAt: at}})
	if e != nil {
		if code(e) == "idempotency_conflict" {
			return out, e
		}
		kind = "database_acceptance"
		return out, &Error{Code: "temporarily_unavailable", Cause: e}
	}
	fresh := v.Operation.ID == op.ID
	out, kind, err = s.execute(ctx, v, &externalBudget{s: s}, true)
	out.NewlyAccepted = fresh
	return
}
func (s *Service) Continue(ctx context.Context, id, key string) (out Outcome, err error) {
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	ctx = context.WithValue(ctx, actionKey{}, "continue")
	kind := ""
	defer func() { s.log(ctx, "continue", out, err, kind) }()
	if !CanonicalID(id) || !CanonicalID(key) {
		return out, &Error{Code: "invalid_request"}
	}
	target := "/api/orders/" + id + "/checkout"
	b, e := s.repo.LoadBinding(ctx, key)
	if e == nil {
		if b.Method != "POST" || b.Target != target || b.OrderID != id {
			return out, &Error{Code: "idempotency_conflict"}
		}
		if b.View.Order.Status == "paid" {
			out = s.outcome(b.View)
			return out, failure("checkout_blocked", b.View, nil)
		}
		out, kind, err = s.execute(ctx, b.View, &externalBudget{s: s}, true)
		return
	}
	if code(e) != "not_found" {
		kind = "database_read"
		return out, &Error{Code: "temporarily_unavailable", Cause: e}
	}
	v, e := s.repo.LoadOrder(ctx, id)
	if e != nil {
		if code(e) == "not_found" {
			return out, e
		}
		kind = "database_read"
		return out, &Error{Code: "temporarily_unavailable", OrderID: id, Cause: e}
	}
	budget := &externalBudget{s: s}
	out = s.outcome(v)
	if v.Order.Status == "paid" || v.Operation.State == "paid" || !s.compatible(v.Operation) || v.Operation.State == "complete_unpaid" && v.Operation.SessionID == nil || v.Operation.State == "rejected" && v.Operation.PriorAmbiguity {
		return out, failure("checkout_blocked", v, nil)
	}
	if v.Operation.SessionID != nil {
		out, kind, err = s.execute(ctx, v, budget, false)
		if kind == "ownership_lost" {
			return out, failure("checkout_blocked", View{Order: out.Order, Operation: out.Operation}, nil)
		}
		if err != nil && code(err) != "checkout_rejected" {
			return
		}
		v = View{Order: out.Order, Operation: out.Operation}
		err = nil
		out = s.outcome(v)
	}
	if !out.CanResume && !out.CanRetrySameOperation && !out.CanStartNewAttempt && v.Operation.OwnerToken == "" {
		return out, failure("checkout_blocked", v, nil)
	}
	op := v.Operation
	if out.CanStartNewAttempt {
		op, e = s.intent(v.Order)
		if e != nil {
			return out, failure("temporarily_unavailable", v, e)
		}
	}
	at := s.opts.Now().UTC().Truncate(time.Microsecond)
	binding := RequestBinding{Key: key, Method: "POST", Target: target, OrderID: id, OperationID: op.ID, Description: v.Order.Description, Amount: v.Order.Amount, Currency: v.Order.Currency, CreatedAt: at}
	bindCtx := ctx
	cancelBind := func() {}
	if op.ID == v.Operation.ID && op.SessionID != nil || !budget.available(ctx) {
		bindCtx, cancelBind = s.finalContext(ctx)
	}
	bound, e := s.repo.BindContinuation(bindCtx, ContinuationIntent{OrderID: id, ExpectedCurrentOperationID: v.Order.CurrentOperationID, ExpectedVersion: v.Operation.Version, Operation: op, Binding: binding, History: HistoryEntry{OrderID: id, OperationID: op.ID, Kind: "operation_prepared", State: "prepared", ObservedAt: at}})
	cancelBind()
	if e != nil {
		out = s.outcome(bound)
		if out.Order.ID == "" {
			out = s.outcome(v)
		}
		if code(e) == "checkout_blocked" || code(e) == "idempotency_conflict" {
			return out, e
		}
		kind = "database_acceptance"
		return out, failure("temporarily_unavailable", v, e)
	}
	if bound.Operation.SessionID != nil {
		out = s.outcome(bound)
		return out, nil
	}
	out, kind, err = s.execute(ctx, bound, budget, true)
	out.NewlyAccepted = op.ID != v.Operation.ID
	return
}
func (s *Service) Get(ctx context.Context, id string) (v View, err error) {
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	if !CanonicalID(id) {
		return v, &Error{Code: "invalid_request"}
	}
	v, err = s.repo.LoadOrder(ctx, id)
	if err != nil {
		if code(err) != "not_found" {
			err = &Error{Code: "temporarily_unavailable", OrderID: id, Cause: err}
		}
		s.log(ctx, "read", s.outcome(v), err, "database_read")
		return
	}
	v = s.view(v)
	s.log(ctx, "read", s.outcome(v), nil, "")
	return
}
func (s *Service) History(ctx context.Context, id string, after int64, limit int) (p HistoryPage, err error) {
	ctx, cancel := s.bounded(ctx)
	defer cancel()
	if !CanonicalID(id) || after < 0 || limit < 1 || limit > 100 {
		return p, &Error{Code: "invalid_request"}
	}
	p, err = s.repo.ReadHistory(ctx, id, after, limit)
	if err != nil && code(err) != "not_found" {
		err = &Error{Code: "temporarily_unavailable", OrderID: id, Cause: err}
	}
	s.log(ctx, "history", Outcome{Order: Order{ID: id}}, err, "")
	return
}
