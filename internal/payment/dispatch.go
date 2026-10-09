package payment

import (
	"context"
	"errors"
	"time"
)

type externalBudget struct {
	s           *Service
	started     time.Time
	realStarted time.Time
	calls       int
}

func (b *externalBudget) remaining(ctx context.Context) time.Duration {
	if b.started.IsZero() {
		b.started = b.s.opts.Now()
		b.realStarted = time.Now()
	}
	elapsed := b.s.opts.Now().Sub(b.started)
	if real := time.Since(b.realStarted); real > elapsed {
		elapsed = real
	}
	d := b.s.opts.RetryBudget - elapsed
	if end, ok := ctx.Deadline(); ok && time.Until(end) < d {
		d = time.Until(end)
	}
	if end, ok := ctx.Value(workDeadlineKey{}).(time.Time); ok && time.Until(end) < d {
		d = time.Until(end)
	}
	return d
}
func (b *externalBudget) available(ctx context.Context) bool {
	return ctx.Err() == nil && b.calls < b.s.opts.MaxAttempts && b.remaining(ctx) > 0
}
func (b *externalBudget) call(ctx context.Context, op Operation, retrieve bool) (SessionEvidence, error) {
	d := b.remaining(ctx)
	if d > b.s.opts.CallTimeout {
		d = b.s.opts.CallTimeout
	}
	child, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	b.calls++
	if retrieve {
		return b.s.gateway.Retrieve(child, *op.SessionID)
	}
	return b.s.gateway.Create(child, op.Snapshot)
}
func (b *externalBudget) retryWait(ctx context.Context, e SessionEvidence) (time.Duration, bool) {
	if e.StripeShouldRetry != nil && !*e.StripeShouldRetry {
		return 0, false
	}
	switch e.ErrorClass {
	case "transport", "rate_limit", "transient_conflict", "server":
	default:
		return 0, false
	}
	if !b.available(ctx) {
		return 0, false
	}
	d := 250 * time.Millisecond
	if b.calls >= 2 {
		d = 500 * time.Millisecond
	}
	if e.RetryAfter > d {
		d = e.RetryAfter
	}
	return d, d < b.remaining(ctx)
}
func (b *externalBudget) retry(ctx context.Context, e SessionEvidence) bool {
	d, ok := b.retryWait(ctx, e)
	return ok && b.s.opts.Wait(ctx, d) == nil && b.available(ctx)
}
func errorKind(e SessionEvidence, err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "transport_timeout"
	}
	if e.ErrorClass != "" {
		return e.ErrorClass
	}
	return "temporarily_unavailable"
}
func evidenceState(op Operation, e SessionEvidence) (string, bool) {
	if !correlated(op, e) {
		return "unresolved", false
	}
	if e.PaymentStatus != "unpaid" {
		return "unresolved", false
	}
	switch e.Status {
	case "open":
		if e.URL == "" || e.ExpiresAt == 0 {
			return "unresolved", false
		}
		return "open", true
	case "complete":
		return "complete_unpaid", true
	case "expired":
		return "expired", true
	}
	return "unresolved", false
}
func (s *Service) release(ctx context.Context, v View) {
	op := v.Operation
	if op.OwnerToken == "" {
		return
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if !deadline.After(time.Now()) {
		return
	}
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	releaser, ok := s.repo.(interface {
		ReleaseDispatch(context.Context, Observation) error
	})
	if !ok {
		return
	}
	err := releaser.ReleaseDispatch(cleanup, Observation{OrderID: v.Order.ID, OperationID: op.ID, ExpectedCurrentOperationID: v.Order.CurrentOperationID, ExpectedVersion: op.Version, OwnerToken: op.OwnerToken, State: op.State, PriorAmbiguity: op.PriorAmbiguity, History: HistoryEntry{FailureCode: op.FailureCode}})
	if err != nil {
		kind := "database_result"
		if code(err) == "ownership_lost" {
			kind = "ownership_lost"
		}
		action, _ := ctx.Value(actionKey{}).(string)
		s.log(ctx, action, s.outcome(v), err, kind)
	}
}
func (s *Service) readback(ctx context.Context, v View, kind string, cause error) (Outcome, string, error) {
	ctx, cancel := s.finalContext(ctx)
	defer cancel()
	s.release(ctx, v)
	if ctx.Err() != nil {
		category := errorKind(SessionEvidence{}, ctx.Err())
		if kind == "database_result" || kind == "database_dispatch" {
			category = kind
		}
		return s.outcome(v), category, failure("temporarily_unavailable", v, ctx.Err())
	}
	fresh, err := s.repo.LoadOrder(ctx, v.Order.ID)
	if err != nil {
		return s.outcome(v), "database_read", failure("temporarily_unavailable", v, err)
	}
	if fresh.Operation.ID != v.Operation.ID {
		return s.outcome(fresh), "ownership_lost", nil
	}
	out := s.outcome(fresh)
	if cause != nil && ctx.Err() != nil {
		return out, kind, failure("temporarily_unavailable", v, cause)
	}
	return out, kind, nil
}
func (s *Service) execute(ctx context.Context, v View, b *externalBudget, accepted bool) (Outcome, string, error) {
	out := s.outcome(v)
	op := v.Operation
	if v.Order.Status == "paid" || op.State == "paid" {
		return out, "", nil
	}
	if op.State == "rejected" && !op.PriorAmbiguity {
		kind := "rejected"
		if op.FailureCode != nil {
			kind = *op.FailureCode
		}
		return out, kind, failure("checkout_rejected", v, nil)
	}
	if op.ID != v.Order.CurrentOperationID {
		return out, "", nil
	}
	if !s.compatible(op) {
		return out, "unsupported_snapshot", nil
	}
	if op.OwnerToken != "" {
		return out, "ownership_lost", nil
	}
	retrieve := op.SessionID != nil
	if !retrieve && (op.State != "prepared" && op.State != "unresolved" || op.State == "unresolved" && !s.safe(op)) {
		return out, "safe_age_exhausted", nil
	}
	// Bound historical identities may be inspected, but cannot dispatch a mutation.
	if !retrieve && op.ID != v.Order.CurrentOperationID {
		return out, "ownership_lost", nil
	}
	prior := op.PriorAmbiguity || op.FirstDispatchAt != nil
	for b.available(ctx) {
		if !retrieve {
			if op.FirstDispatchAt != nil && !s.safe(op) {
				return s.outcome(v), "safe_age_exhausted", nil
			}
			at := s.opts.Now().UTC().Truncate(time.Microsecond)
			snap := op.Snapshot
			first := at
			if op.FirstDispatchAt != nil {
				first = *op.FirstDispatchAt
			}
			if snap.FirstDispatchAt == nil {
				snap.FirstDispatchAt = ptr(first)
				snap.ExpiresAt = first.Add(23*time.Hour + 59*time.Minute).Unix()
			}
			snap.LastDispatchAt = ptr(at)
			token, err := identity()
			if err != nil {
				return out, "temporarily_unavailable", failure("temporarily_unavailable", v, err)
			}
			prepared, err := s.repo.PrepareDispatch(ctx, Dispatch{OrderID: v.Order.ID, OperationID: op.ID, ExpectedVersion: op.Version, ExpectedCurrentOperationID: v.Order.CurrentOperationID, OwnerToken: token, Snapshot: snap, FirstDispatchAt: first, LastDispatchAt: at})
			if err != nil {
				if code(err) == "ownership_lost" || code(err) == "checkout_blocked" {
					return s.readback(ctx, v, "ownership_lost", err)
				}
				return s.readback(ctx, v, "database_dispatch", err)
			}
			op = prepared
			v.Operation = op
		}
		// Database work can consume the remaining idempotency safety window.
		// Check the committed first dispatch again at the physical send boundary.
		if !retrieve && !s.safe(op) {
			return s.readback(ctx, v, "safe_age_exhausted", nil)
		}
		e, callErr := b.call(ctx, op, retrieve)
		state := "unresolved"
		kind := ""
		valid := false
		ambiguous := prior
		if callErr == nil {
			state, valid = evidenceState(op, e)
			if valid {
				ambiguous = false
			}
			if !valid {
				if correlated(op, e) && e.Status == "complete" && e.PaymentStatus == "paid" && e.PaymentIntentID != nil && *e.PaymentIntentID != "" {
					kind = "confirmation_required"
					e.URL = ""
					e.ErrorClass = kind
					ambiguous = false
				} else {
					kind = "evidence_mismatch"
					e = SessionEvidence{ErrorClass: kind}
					ambiguous = true
				}
			}
		} else {
			kind = errorKind(e, callErr)
			class := e.ErrorClass
			if !retrieve && !prior && (class == "validation" || class == "credential" || class == "permission") {
				state = "rejected"
				ambiguous = false
			} else {
				ambiguous = true
			}
			if kind == "transport_timeout" {
				e.ErrorClass = "transport"
			}
		}
		if ctx.Err() != nil {
			return s.readback(ctx, v, errorKind(e, ctx.Err()), ctx.Err())
		}
		history := HistoryEntry{OrderID: v.Order.ID, OperationID: op.ID, Kind: "operation_state_changed", State: state, SessionID: optionalString(e.SessionID), PaymentIntentID: e.PaymentIntentID, RequestID: optionalString(e.RequestID), ObservedAt: s.opts.Now().UTC().Truncate(time.Microsecond), FromState: ptr(op.State), ToState: ptr(state)}
		if kind != "" {
			history.FailureCode = ptr(e.ErrorClass)
			if e.ErrorClass == "" {
				history.FailureCode = ptr(kind)
			}
		}
		observeCtx := ctx
		cancelObserve := func() {}
		_, canRetry := b.retryWait(ctx, e)
		if callErr == nil && (accepted || !b.available(ctx)) || callErr != nil && !canRetry {
			observeCtx, cancelObserve = s.finalContext(ctx)
		}
		observed, err := s.repo.ApplyObservation(observeCtx, Observation{OrderID: v.Order.ID, OperationID: op.ID, ExpectedVersion: op.Version, ExpectedCurrentOperationID: v.Order.CurrentOperationID, OwnerToken: op.OwnerToken, State: state, Evidence: e, PriorAmbiguity: ambiguous, History: history})
		if err != nil {
			kind := "database_result"
			if code(err) == "ownership_lost" {
				kind = "ownership_lost"
			}
			out, category, readErr := s.readback(observeCtx, v, kind, err)
			cancelObserve()
			return out, category, readErr
		}
		cancelObserve()
		v = observed
		op = v.Operation
		out = s.outcome(v)
		if valid {
			return out, "", nil
		}
		if state == "rejected" {
			return out, e.ErrorClass, failure("checkout_rejected", v, callErr)
		}
		if callErr == nil {
			if !accepted {
				return out, kind, failure("checkout_blocked", v, nil)
			}
			return out, kind, nil
		}
		if !b.retry(ctx, e) {
			if !accepted {
				return out, kind, failure("checkout_blocked", v, callErr)
			}
			return out, kind, nil
		}
		prior = true
	}
	return s.outcome(v), "deferred_budget", nil
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func correlated(op Operation, e SessionEvidence) bool {
	s := op.Snapshot
	return e.SessionID != "" && e.ClientReferenceID == op.OrderID && e.Metadata["order_id"] == op.OrderID && e.Metadata["operation_id"] == op.ID && e.AmountTotal == s.Amount && e.Currency == s.Currency && e.Mode == "payment" && !e.Livemode && (op.SessionID == nil || *op.SessionID == e.SessionID) && (op.PaymentIntentID == nil || e.PaymentIntentID != nil && *op.PaymentIntentID == *e.PaymentIntentID)
}
