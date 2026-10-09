package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

type Wire struct {
	Method string
	Path   string
	Header http.Header
	Form   url.Values
	At     time.Time
}
type StripeServer struct {
	Server    *httptest.Server
	mu        sync.Mutex
	wires     []Wire
	objects   map[string]map[string]any
	Status    int
	ErrorBody string
	Mutation  func(map[string]any)
	Before    func(context.Context, Wire)
	Drop      bool
}

func NewStripeServer(t *testing.T) *StripeServer {
	t.Helper()
	s := &StripeServer{objects: map[string]map[string]any{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Server.Close)
	return s
}
func (s *StripeServer) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	wire := Wire{r.Method, r.URL.Path, r.Header.Clone(), r.Form, time.Now()}
	s.mu.Lock()
	s.wires = append(s.wires, wire)
	before := s.Before
	status := s.Status
	body := s.ErrorBody
	drop := s.Drop
	s.mu.Unlock()
	if before != nil {
		before(r.Context(), wire)
	}
	if r.Context().Err() != nil {
		return
	}
	if status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_test_fault")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.Header.Get("Idempotency-Key")
	obj := s.objects[key]
	if r.Method == "GET" {
		for _, candidate := range s.objects {
			if "/v1/checkout/sessions/"+candidate["id"].(string) == r.URL.Path {
				obj = candidate
				break
			}
		}
	}
	if obj == nil {
		if r.Method == "GET" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"missing"}}`))
			return
		}
		id := fmt.Sprintf("cs_test_%d", len(s.objects)+1)
		amount := r.Form.Get("line_items[0][price_data][unit_amount]")
		var cents int64
		_, _ = fmt.Sscan(amount, &cents)
		obj = map[string]any{"id": id, "object": "checkout.session", "client_reference_id": r.Form.Get("client_reference_id"), "metadata": map[string]string{"order_id": r.Form.Get("metadata[order_id]"), "operation_id": r.Form.Get("metadata[operation_id]")}, "amount_total": cents, "currency": r.Form.Get("line_items[0][price_data][currency]"), "mode": "payment", "livemode": false, "status": "open", "payment_status": "unpaid", "url": "https://checkout.stripe.com/c/pay/" + id, "expires_at": time.Now().Add(24 * time.Hour).Unix()}
		s.objects[key] = obj
	}
	if s.Mutation != nil {
		s.Mutation(obj)
	}
	if drop {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Request-Id", "req_test_checkout")
	_ = json.NewEncoder(w).Encode(obj)
}
func (s *StripeServer) Wires() []Wire {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Wire(nil), s.wires...)
}
func (s *StripeServer) LogicalObjects() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.objects) }
func SessionJSON(e payment.SessionEvidence) map[string]any {
	return map[string]any{"id": e.SessionID, "object": "checkout.session", "client_reference_id": e.ClientReferenceID, "metadata": e.Metadata, "amount_total": e.AmountTotal, "currency": e.Currency, "mode": e.Mode, "livemode": e.Livemode, "status": e.Status, "payment_status": e.PaymentStatus, "payment_intent": e.PaymentIntentID, "url": e.URL, "expires_at": e.ExpiresAt}
}
func CheckWire(t *testing.T, w Wire, s payment.Snapshot) {
	t.Helper()
	require.Equal(t, "POST", w.Method)
	require.Equal(t, "/v1/checkout/sessions", w.Path)
	require.Equal(t, s.StripeKey, w.Header.Get("Idempotency-Key"))
	require.Equal(t, s.APIVersion, w.Header.Get("Stripe-Version"))
	require.Equal(t, "payment", w.Form.Get("mode"))
	require.Equal(t, "hosted_page", w.Form.Get("ui_mode"))
	require.Equal(t, "card", w.Form.Get("payment_method_types[0]"))
	require.Equal(t, s.OrderID, w.Form.Get("client_reference_id"))
	require.Equal(t, s.OrderID, w.Form.Get("metadata[order_id]"))
	require.Equal(t, s.OperationID, w.Form.Get("metadata[operation_id]"))
	require.Equal(t, s.Currency, w.Form.Get("line_items[0][price_data][currency]"))
	require.Equal(t, fmt.Sprint(s.Amount), w.Form.Get("line_items[0][price_data][unit_amount]"))
	require.Equal(t, "1", w.Form.Get("line_items[0][quantity]"))
	require.Equal(t, s.Description, w.Form.Get("line_items[0][price_data][product_data][name]"))
	require.Equal(t, s.SuccessURL, w.Form.Get("success_url"))
	require.Equal(t, s.CancelURL, w.Form.Get("cancel_url"))
	require.Equal(t, fmt.Sprint(s.ExpiresAt), w.Form.Get("expires_at"))
	require.Equal(t, "automatic", w.Form.Get("payment_intent_data[capture_method]"))
}
