package payment

import (
	"context"
	"sync"
	"time"
)

// This repository records persistence boundaries. It does not decide eligibility,
// evidence validity, flags, or retry classification; those are service assertions.
type policyRepoState struct {
	Orders     map[string]Order
	Operations map[string]Operation
	Bindings   map[string]RequestBinding
	History    map[string][]HistoryEntry
}
type policyRepository struct {
	mu          sync.Mutex
	state       policyRepoState
	calls       []string
	hook        func(context.Context, string) error
	beforeBind  func(*policyRepoState)
	beforeApply func(*policyRepoState)
}

func newPolicyRepository(views ...View) *policyRepository {
	r := &policyRepository{state: policyRepoState{Orders: map[string]Order{}, Operations: map[string]Operation{}, Bindings: map[string]RequestBinding{}, History: map[string][]HistoryEntry{}}}
	for _, v := range views {
		r.state.Orders[v.Order.ID] = v.Order
		r.state.Operations[v.Operation.ID] = clonePolicyOperation(v.Operation)
	}
	return r
}
func clonePolicyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func clonePolicyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	n := map[string]string{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func clonePolicySnapshot(s Snapshot) Snapshot {
	s.FirstDispatchAt = clonePolicyPtr(s.FirstDispatchAt)
	s.LastDispatchAt = clonePolicyPtr(s.LastDispatchAt)
	return s
}
func clonePolicyOperation(o Operation) Operation {
	o.Snapshot = clonePolicySnapshot(o.Snapshot)
	o.SessionID = clonePolicyPtr(o.SessionID)
	o.PaymentIntentID = clonePolicyPtr(o.PaymentIntentID)
	o.CheckoutURL = clonePolicyPtr(o.CheckoutURL)
	o.ExpiresAt = clonePolicyPtr(o.ExpiresAt)
	o.FirstDispatchAt = clonePolicyPtr(o.FirstDispatchAt)
	o.LastDispatchAt = clonePolicyPtr(o.LastDispatchAt)
	o.FailureCode = clonePolicyPtr(o.FailureCode)
	return o
}
func clonePolicyHistory(h HistoryEntry) HistoryEntry {
	h.SessionID = clonePolicyPtr(h.SessionID)
	h.PaymentIntentID = clonePolicyPtr(h.PaymentIntentID)
	h.EventID = clonePolicyPtr(h.EventID)
	h.RequestID = clonePolicyPtr(h.RequestID)
	h.EventAt = clonePolicyPtr(h.EventAt)
	h.FromState = clonePolicyPtr(h.FromState)
	h.ToState = clonePolicyPtr(h.ToState)
	h.FailureCode = clonePolicyPtr(h.FailureCode)
	return h
}
func (r *policyRepository) State() policyRepoState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := policyRepoState{Orders: map[string]Order{}, Operations: map[string]Operation{}, Bindings: map[string]RequestBinding{}, History: map[string][]HistoryEntry{}}
	for k, v := range r.state.Orders {
		s.Orders[k] = v
	}
	for k, v := range r.state.Operations {
		s.Operations[k] = clonePolicyOperation(v)
	}
	for k, v := range r.state.Bindings {
		s.Bindings[k] = v
	}
	for k, entries := range r.state.History {
		for _, h := range entries {
			s.History[k] = append(s.History[k], clonePolicyHistory(h))
		}
	}
	return s
}
func (r *policyRepository) record(ctx context.Context, method string) error {
	r.mu.Lock()
	r.calls = append(r.calls, method)
	hook := r.hook
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if hook != nil {
		return hook(ctx, method)
	}
	return nil
}
func (r *policyRepository) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}
func (r *policyRepository) view(id string) (View, error) {
	o, ok := r.state.Orders[id]
	if !ok {
		return View{}, &Error{Code: "not_found"}
	}
	return View{Order: o, Operation: clonePolicyOperation(r.state.Operations[o.CurrentOperationID])}, nil
}
func (r *policyRepository) addHistory(h HistoryEntry) {
	h.Sequence = int64(len(r.state.History[h.OrderID]) + 1)
	r.state.History[h.OrderID] = append(r.state.History[h.OrderID], clonePolicyHistory(h))
}
func (r *policyRepository) LoadBinding(ctx context.Context, key string) (RequestBinding, error) {
	if err := r.record(ctx, "LoadBinding"); err != nil {
		return RequestBinding{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.state.Bindings[key]
	if !ok {
		return RequestBinding{}, &Error{Code: "not_found"}
	}
	v, err := r.view(b.OrderID)
	if err != nil {
		return RequestBinding{}, err
	}
	v.Operation = clonePolicyOperation(r.state.Operations[b.OperationID])
	b.View = v
	return b, nil
}
func (r *policyRepository) LoadOrder(ctx context.Context, id string) (View, error) {
	if err := r.record(ctx, "LoadOrder"); err != nil {
		return View{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.view(id)
}
func (r *policyRepository) AcceptInitial(ctx context.Context, in AcceptedIntent) (View, error) {
	if err := r.record(ctx, "AcceptInitial"); err != nil {
		return View{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.state.Bindings[in.Binding.Key]; ok {
		if b.Description != in.Binding.Description || b.Amount != in.Binding.Amount || b.Method != in.Binding.Method || b.Target != in.Binding.Target {
			return View{}, &Error{Code: "idempotency_conflict"}
		}
		return r.view(b.OrderID)
	}
	r.state.Orders[in.Order.ID] = in.Order
	r.state.Operations[in.Operation.ID] = clonePolicyOperation(in.Operation)
	r.state.Bindings[in.Binding.Key] = in.Binding
	r.addHistory(in.History)
	return r.view(in.Order.ID)
}
func (r *policyRepository) BindContinuation(ctx context.Context, in ContinuationIntent) (View, error) {
	if err := r.record(ctx, "BindContinuation"); err != nil {
		return View{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeBind != nil {
		r.beforeBind(&r.state)
	}
	v, err := r.view(in.OrderID)
	if err != nil {
		return View{}, err
	}
	if v.Order.Status == "paid" || v.Order.CurrentOperationID != in.ExpectedCurrentOperationID || v.Operation.Version != in.ExpectedVersion {
		return v, &Error{Code: "checkout_blocked", OrderID: v.Order.ID, OperationID: v.Operation.ID}
	}
	if b, ok := r.state.Bindings[in.Binding.Key]; ok {
		if b.OrderID != in.OrderID || b.OperationID != in.Operation.ID {
			return View{}, &Error{Code: "idempotency_conflict"}
		}
		return r.view(in.OrderID)
	}
	r.state.Bindings[in.Binding.Key] = in.Binding
	if in.Operation.ID != v.Operation.ID {
		o := v.Order
		o.CurrentOperationID = in.Operation.ID
		r.state.Orders[o.ID] = o
		r.state.Operations[in.Operation.ID] = clonePolicyOperation(in.Operation)
		r.addHistory(in.History)
	}
	return r.view(in.OrderID)
}
func (r *policyRepository) PrepareDispatch(ctx context.Context, in Dispatch) (Operation, error) {
	if err := r.record(ctx, "PrepareDispatch"); err != nil {
		return Operation{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, err := r.view(in.OrderID)
	if err != nil {
		return Operation{}, err
	}
	op := r.state.Operations[in.OperationID]
	if v.Order.Status == "paid" || v.Order.CurrentOperationID != in.ExpectedCurrentOperationID || op.Version != in.ExpectedVersion || op.OwnerToken != "" {
		return clonePolicyOperation(op), &Error{Code: "ownership_lost", OrderID: v.Order.ID, OperationID: op.ID}
	}
	op.State = "unresolved"
	op.Snapshot = clonePolicySnapshot(in.Snapshot)
	op.FirstDispatchAt = clonePolicyPtr(&in.FirstDispatchAt)
	op.LastDispatchAt = clonePolicyPtr(&in.LastDispatchAt)
	op.OwnerToken = in.OwnerToken
	op.Version++
	r.state.Operations[op.ID] = op
	r.addHistory(HistoryEntry{OrderID: op.OrderID, OperationID: op.ID, Kind: "dispatch_started", State: "unresolved", ObservedAt: in.LastDispatchAt})
	return clonePolicyOperation(op), nil
}
func (r *policyRepository) ApplyObservation(ctx context.Context, in Observation) (View, error) {
	if err := r.record(ctx, "ApplyObservation"); err != nil {
		return View{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeApply != nil {
		r.beforeApply(&r.state)
	}
	v, err := r.view(in.OrderID)
	if err != nil {
		return View{}, err
	}
	op := r.state.Operations[in.OperationID]
	if v.Order.Status == "paid" || v.Order.CurrentOperationID != in.ExpectedCurrentOperationID || op.Version != in.ExpectedVersion || op.OwnerToken != in.OwnerToken {
		return v, &Error{Code: "ownership_lost", OrderID: v.Order.ID, OperationID: op.ID}
	}
	changed := op.State != in.State
	op.State = in.State
	op.PriorAmbiguity = in.PriorAmbiguity
	op.OwnerToken = ""
	op.Version++
	op.EvidenceSource = "stripe"
	if in.Evidence.SessionID != "" {
		op.SessionID = policyPtr(in.Evidence.SessionID)
	}
	if in.Evidence.PaymentIntentID != nil {
		op.PaymentIntentID = clonePolicyPtr(in.Evidence.PaymentIntentID)
	}
	if in.Evidence.URL != "" {
		op.CheckoutURL = policyPtr(in.Evidence.URL)
	}
	if in.Evidence.ExpiresAt != 0 {
		expiry := time.Unix(in.Evidence.ExpiresAt, 0).UTC()
		op.ExpiresAt = &expiry
	}
	op.FailureCode = clonePolicyPtr(in.History.FailureCode)
	r.state.Operations[op.ID] = op
	if changed {
		r.addHistory(in.History)
	}
	return r.view(in.OrderID)
}
func (r *policyRepository) ReadHistory(ctx context.Context, id string, after int64, limit int) (HistoryPage, error) {
	if err := r.record(ctx, "ReadHistory"); err != nil {
		return HistoryPage{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.state.Orders[id]; !ok {
		return HistoryPage{}, &Error{Code: "not_found"}
	}
	p := HistoryPage{OrderID: id, Entries: []HistoryEntry{}}
	for _, h := range r.state.History[id] {
		if h.Sequence > after {
			if len(p.Entries) == limit {
				p.NextAfter = policyPtr(p.Entries[len(p.Entries)-1].Sequence)
				break
			}
			p.Entries = append(p.Entries, clonePolicyHistory(h))
		}
	}
	return p, nil
}
