package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/filser89/stripe-payments-go/internal/payment"
)

type checkoutAPI struct {
	ops     payment.Operations
	timeout time.Duration
	logger  *slog.Logger
	root    http.Handler
}

func NewCheckoutHandler(ops payment.Operations, timeout time.Duration, logger *slog.Logger) http.Handler {
	return &checkoutAPI{ops, timeout, logger, landing(logger)}
}
func (h *checkoutAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		h.root.ServeHTTP(w, r)
		return
	}
	admitted := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	ctx = payment.WithRequestBudget(ctx, admitted.Add(h.timeout-time.Second))
	r = r.WithContext(ctx)
	control := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := control.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.logger.WarnContext(ctx, "read deadline unavailable", "error_kind", "transport_deadline")
	}
	if err := control.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.logger.WarnContext(ctx, "write deadline unavailable", "error_kind", "transport_deadline")
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(joined)
		_ = control.SetReadDeadline(time.Now())
		_ = control.SetWriteDeadline(time.Now())
	})
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	action := "read"
	path := r.URL.Path
	id := ""
	route := ""
	if path == "/api/orders" {
		route = "create"
		action = "create"
	} else if strings.HasPrefix(path, "/api/orders/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/orders/"), "/")
		if len(parts) == 1 {
			route = "read"
			id = parts[0]
		} else if len(parts) == 2 && (parts[1] == "history" || parts[1] == "checkout") {
			route = parts[1]
			id = parts[0]
			if route == "checkout" {
				action = "continue"
			} else {
				action = "history"
			}
		}
	}
	if route == "" {
		h.local(w, r, 404, "not_found", action)
		return
	}
	if id != "" && !payment.CanonicalID(id) {
		h.local(w, r, 400, "invalid_request", action)
		return
	}
	mutate := route == "create" || route == "checkout"
	allow := "GET, HEAD"
	validMethod := r.Method == "GET" || r.Method == "HEAD"
	if mutate {
		allow = "POST"
		validMethod = r.Method == "POST"
	}
	if !validMethod {
		w.Header().Set("Allow", allow)
		h.local(w, r, 405, "method_not_allowed", action)
		return
	}
	after, limit, ok := historyQuery(r.URL, route == "history")
	if !ok {
		h.local(w, r, 400, "invalid_request", action)
		return
	}
	if mutate {
		media, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || media != "application/json" || len(params) > 0 && (len(params) != 1 || !strings.EqualFold(params["charset"], "utf-8")) || r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			h.local(w, r, 415, "unsupported_media_type", action)
			return
		}
		data, e := io.ReadAll(io.LimitReader(r.Body, 4097))
		if len(data) > 4096 {
			w.Header().Set("Connection", "close")
			h.local(w, r, 413, "body_too_large", action)
			return
		}
		if e != nil {
			h.localKind(w, r, 400, "invalid_request", action, "body_read")
			return
		}
		in, ok := checkoutInput(data, route == "create")
		if !ok {
			h.local(w, r, 400, "invalid_request", action)
			return
		}
		if ctx.Err() != nil {
			h.localKind(w, r, 400, "invalid_request", action, "body_read")
			return
		}
		var out payment.Outcome
		var err error
		if route == "create" {
			out, err = h.ops.Create(ctx, in)
		} else {
			out, err = h.ops.Continue(ctx, id, in.RequestKey)
		}
		if err != nil {
			h.domainError(w, r, err, action, out)
			return
		}
		status := 200
		if out.NewlyAccepted {
			status = 201
		}
		if out.Pending {
			status = 202
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Location", "/api/orders/"+out.Order.ID)
		}
		result := "established"
		if out.NewlyAccepted {
			result = "accepted"
		}
		if out.Pending {
			result = "pending"
		}
		h.record(r, w, action, result, status, out.Order.ID, out.Operation.ID, out.Operation.State, out.Order.Status, optionalFailure(out.Operation.FailureCode))
		h.json(w, r, status, viewJSON(out.Order, out.Operation, out.CanResume, out.CanRetrySameOperation, out.CanStartNewAttempt, out.NeedsInvestigation))
		return
	}
	var one [1]byte
	n, e := r.Body.Read(one[:])
	if n > 0 || e != io.EOF {
		w.Header().Set("Connection", "close")
		kind := "invalid_request"
		if e != nil && e != io.EOF {
			kind = "body_read"
		}
		h.localKind(w, r, 400, "invalid_request", action, kind)
		return
	}
	if route == "read" {
		v, e := h.ops.Get(ctx, id)
		if e != nil {
			h.domainError(w, r, e, action)
			return
		}
		h.record(r, w, action, "established", 200, v.Order.ID, v.Operation.ID, v.Operation.State, v.Order.Status, "")
		h.json(w, r, 200, viewJSON(v.Order, v.Operation, v.CanResume, v.CanRetrySameOperation, v.CanStartNewAttempt, v.NeedsInvestigation))
		return
	}
	p, e := h.ops.History(ctx, id, after, limit)
	if e != nil {
		h.domainError(w, r, e, action)
		return
	}
	entries := make([]map[string]any, 0, len(p.Entries))
	for _, e := range p.Entries {
		var op any
		if e.OperationID != "" {
			op = e.OperationID
		}
		entries = append(entries, map[string]any{"sequence": e.Sequence, "kind": e.Kind, "recorded_at": stamp(e.ObservedAt), "order_id": e.OrderID, "operation_id": op, "from_state": e.FromState, "to_state": e.ToState, "stripe_session_id": e.SessionID, "stripe_payment_intent_id": e.PaymentIntentID, "stripe_event_id": e.EventID, "stripe_request_id": e.RequestID, "failure_code": e.FailureCode})
	}
	h.record(r, w, action, "established", 200, p.OrderID, "", "", "", "")
	h.json(w, r, 200, map[string]any{"order_id": p.OrderID, "entries": entries, "next_after": p.NextAfter})
}
func checkoutInput(data []byte, initial bool) (payment.CreateInput, bool) {
	var out payment.CreateInput
	if !utf8.Valid(data) {
		return out, false
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return out, false
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		t, e := d.Token()
		if e != nil {
			return out, false
		}
		k, ok := t.(string)
		if !ok {
			return out, false
		}
		if _, ok := fields[k]; ok {
			return out, false
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return out, false
		}
		fields[k] = raw
	}
	if t, e := d.Token(); e != nil || t != json.Delim('}') {
		return out, false
	}
	if _, e := d.Token(); e != io.EOF {
		return out, false
	}
	want := 1
	if initial {
		want = 3
	}
	if len(fields) != want {
		return out, false
	}
	raw, ok := fields["request_key"]
	if !ok || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &out.RequestKey) != nil || !payment.CanonicalID(out.RequestKey) {
		return out, false
	}
	if !initial {
		return out, true
	}
	raw, ok = fields["description"]
	if !ok || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &out.Description) != nil || !payment.ValidDescription(out.Description) {
		return out, false
	}
	raw, ok = fields["amount"]
	if !ok || len(raw) == 0 {
		return out, false
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			return out, false
		}
	}
	n, e := strconv.ParseInt(string(raw), 10, 64)
	if e != nil || n < 50 || n > 100000 {
		return out, false
	}
	out.Amount = n
	return out, true
}
func historyQuery(u *url.URL, history bool) (int64, int, bool) {
	after := int64(0)
	limit := 50
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return 0, 0, false
	}
	for k, v := range q {
		if !history || len(v) != 1 || k != "after" && k != "limit" || v[0] == "" {
			return 0, 0, false
		}
		for _, r := range v[0] {
			if r < '0' || r > '9' {
				return 0, 0, false
			}
		}
		n, e := strconv.ParseInt(v[0], 10, 64)
		if e != nil {
			return 0, 0, false
		}
		if k == "after" {
			after = n
		} else {
			if n < 1 || n > 100 {
				return 0, 0, false
			}
			limit = int(n)
		}
	}
	return after, limit, true
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func optionalStamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}
func viewJSON(o payment.Order, p payment.Operation, resume, retry, fresh, investigate bool) map[string]any {
	return map[string]any{"order": map[string]any{"id": o.ID, "description": o.Description, "amount": o.Amount, "currency": o.Currency, "payment_status": o.Status, "created_at": stamp(o.CreatedAt), "updated_at": stamp(o.UpdatedAt)}, "operation": map[string]any{"id": p.ID, "state": p.State, "stripe_session_id": p.SessionID, "stripe_payment_intent_id": p.PaymentIntentID, "checkout_url": p.CheckoutURL, "first_dispatch_at": optionalStamp(p.FirstDispatchAt), "expires_at": optionalStamp(p.ExpiresAt), "created_at": stamp(p.CreatedAt), "updated_at": stamp(p.UpdatedAt), "failure_code": p.FailureCode, "investigation_required": investigate}, "can_resume": resume, "can_retry_same_operation": retry, "can_start_new_attempt": fresh}
}
func (h *checkoutAPI) json(w http.ResponseWriter, r *http.Request, status int, value any) {
	if deadline := payment.ResponseDeadline(r.Context()); !deadline.IsZero() {
		if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			h.logger.WarnContext(r.Context(), "response deadline failed", "error_kind", "transport_deadline")
		}
	}
	data, e := json.Marshal(value)
	if e != nil {
		h.logger.ErrorContext(r.Context(), "JSON rendering failed", "error_kind", "response_render")
		return
	}
	data = append(data, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		if _, e := w.Write(data); e != nil {
			h.logger.WarnContext(r.Context(), "response write failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "response_write")
		}
		if e := http.NewResponseController(w).Flush(); e != nil && !errors.Is(e, http.ErrNotSupported) {
			h.logger.WarnContext(r.Context(), "response flush failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "response_write")
		}
	}
}
func (h *checkoutAPI) record(r *http.Request, w http.ResponseWriter, action, outcome string, status int, order, op, state, pay, kind string) {
	a := []any{"request_id", w.Header().Get("X-Request-ID"), "action", action, "outcome", outcome, "status", status}
	if order != "" {
		a = append(a, "order_id", order)
	}
	if op != "" {
		a = append(a, "operation_id", op)
	}
	if state != "" {
		a = append(a, "operation_state", state)
	}
	if pay != "" {
		a = append(a, "payment_status", pay)
	}
	if kind != "" {
		a = append(a, "error_kind", kind, "failure_code", kind)
	}
	h.logger.InfoContext(r.Context(), "checkout outcome", a...)
}
func (h *checkoutAPI) local(w http.ResponseWriter, r *http.Request, status int, code, action string) {
	h.localKind(w, r, status, code, action, code)
}
func (h *checkoutAPI) localKind(w http.ResponseWriter, r *http.Request, status int, code, action, kind string) {
	h.record(r, w, action, "rejected", status, "", "", "", "", kind)
	h.json(w, r, status, map[string]any{"error": map[string]any{"code": code, "message": "Request could not be completed"}})
}
func (h *checkoutAPI) domainError(w http.ResponseWriter, r *http.Request, err error, action string, known ...payment.Outcome) {
	var e *payment.Error
	if !errors.As(err, &e) {
		e = &payment.Error{Code: "temporarily_unavailable"}
	}
	status := 503
	switch e.Code {
	case "invalid_request":
		status = 400
	case "not_found":
		status = 404
	case "checkout_blocked", "idempotency_conflict":
		status = 409
	case "checkout_rejected":
		status = 502
	}
	v := map[string]any{"code": e.Code, "message": "Request could not be completed"}
	if e.OrderID != "" {
		v["order_id"] = e.OrderID
	}
	if e.OperationID != "" {
		v["operation_id"] = e.OperationID
	}
	outcome := "failed"
	if e.Code == "checkout_rejected" {
		outcome = "rejected"
	}
	if e.Code == "checkout_blocked" {
		outcome = "blocked"
	}
	state, pay := "", ""
	kind := e.Code
	if len(known) > 0 {
		state = known[0].Operation.State
		pay = known[0].Order.Status
		if known[0].Operation.FailureCode != nil {
			kind = *known[0].Operation.FailureCode
		}
	}
	h.record(r, w, action, outcome, status, e.OrderID, e.OperationID, state, pay, kind)
	h.json(w, r, status, map[string]any{"error": v})
}

func optionalFailure(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
