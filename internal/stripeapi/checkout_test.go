package stripeapi

import (
	"context"
	"encoding/json"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v87"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSTR001SDKRequest(t *testing.T) { // STR-001 STR-003
	server := testutil.NewStripeServer(t)
	intent := testutil.Intent()
	snap := intent.Operation.Snapshot
	snap.FirstDispatchAt = testutil.Pointer(intent.Order.CreatedAt)
	snap.LastDispatchAt = snap.FirstDispatchAt
	snap.ExpiresAt = intent.Order.CreatedAt.Add(23*time.Hour + 59*time.Minute).Unix()
	gateway := New("sk_test_fixture", Options{BackendURL: server.Server.URL, HTTPClient: server.Server.Client()})
	require.NotNil(t, gateway, "missing real Stripe adapter")
	require.Equal(t, "2026-09-30.endive", stripe.APIVersion)
	evidence, err := gateway.Create(context.Background(), snap)
	require.NoError(t, err)
	require.NotEmpty(t, evidence.SessionID)
	require.Equal(t, snap.OrderID, evidence.ClientReferenceID)
	require.Equal(t, snap.Amount, evidence.AmountTotal)
	require.Equal(t, "usd", evidence.Currency)
	require.False(t, evidence.Livemode)
	wires := server.Wires()
	require.Len(t, wires, 1)
	testutil.CheckWire(t, wires[0], snap)
	form := wires[0].Form
	require.Equal(t, snap.OrderID, form.Get("payment_intent_data[metadata][order_id]"))
	require.Equal(t, snap.OperationID, form.Get("payment_intent_data[metadata][operation_id]"))
	for _, field := range []string{"automatic_tax[enabled]", "allow_promotion_codes", "adaptive_pricing[enabled]", "after_expiration[recovery][enabled]"} {
		require.Equal(t, "false", form.Get(field), field)
	}
	require.Equal(t, "Bearer sk_test_fixture", wires[0].Header.Get("Authorization"))
	require.NotContains(t, form.Encode(), "checkout-fixture-password")
	require.NotContains(t, form.Encode(), "sk_test_fixture")
	_, err = gateway.Create(context.Background(), snap)
	require.NoError(t, err)
	require.Equal(t, 1, server.LogicalObjects())
	require.Len(t, server.Wires(), 2)
	require.Equal(t, wires[0].Form, server.Wires()[1].Form)
	require.Equal(t, wires[0].Header.Get("Idempotency-Key"), server.Wires()[1].Header.Get("Idempotency-Key"))
}
func TestSDKCheckoutEvidenceTranslation(t *testing.T) { // LIFE-002 LIFE-007 STR-008
	for _, tc := range []struct {
		name, status, payment                   string
		live                                    bool
		amount                                  int64
		currency                                string
		session, client, operation, url, intent string
	}{
		{"open", "open", "unpaid", false, 2500, "usd", "cs_test_evidence", "", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"complete_unpaid", "complete", "unpaid", false, 2500, "usd", "cs_test_evidence", "", "", "", "pi_test"},
		{"expired", "expired", "unpaid", false, 2500, "usd", "cs_test_evidence", "", "", "", ""},
		{"paid", "complete", "paid", false, 2500, "usd", "cs_test_evidence", "", "", "", "pi_test"},
		{"no_payment_required", "complete", "no_payment_required", false, 2500, "usd", "cs_test_evidence", "", "", "", ""},
		{"live", "open", "unpaid", true, 2500, "usd", "cs_test_evidence", "", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"wrong_amount", "open", "unpaid", false, 2501, "usd", "cs_test_evidence", "", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"wrong_currency", "open", "unpaid", false, 2500, "eur", "cs_test_evidence", "", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"missing_session", "open", "unpaid", false, 2500, "usd", "", "", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"wrong_client", "open", "unpaid", false, 2500, "usd", "cs_test_evidence", "wrong", "", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"wrong_operation", "open", "unpaid", false, 2500, "usd", "cs_test_evidence", "", "wrong", "https://checkout.stripe.com/c/pay/evidence", ""},
		{"missing_url", "open", "unpaid", false, 2500, "usd", "cs_test_evidence", "", "", "", ""},
		{"unknown_combination", "open", "paid", false, 2500, "usd", "cs_test_evidence", "", "", "https://checkout.stripe.com/c/pay/evidence", "pi_test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewStripeServer(t)
			intent := testutil.Intent()
			snap := intent.Operation.Snapshot
			snap.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
			server.Mutation = func(obj map[string]any) {
				obj["id"] = tc.session
				obj["status"] = tc.status
				obj["payment_status"] = tc.payment
				obj["livemode"] = tc.live
				obj["amount_total"] = tc.amount
				obj["currency"] = tc.currency
				obj["url"] = tc.url
				obj["payment_intent"] = tc.intent
				obj["expires_at"] = snap.ExpiresAt
				if tc.client != "" {
					obj["client_reference_id"] = tc.client
				}
				if tc.operation != "" {
					obj["metadata"] = map[string]string{"order_id": snap.OrderID, "operation_id": tc.operation}
				}
			}
			gateway := New("sk_test_fixture", Options{BackendURL: server.Server.URL, HTTPClient: server.Server.Client()})
			require.NotNil(t, gateway, "missing SDK evidence translation")
			e, err := gateway.Create(context.Background(), snap)
			require.NoError(t, err)
			require.Equal(t, tc.status, e.Status)
			require.Equal(t, tc.payment, e.PaymentStatus)
			require.Equal(t, tc.live, e.Livemode)
			require.Equal(t, tc.amount, e.AmountTotal)
			require.Equal(t, tc.currency, e.Currency)
			require.Equal(t, tc.session, e.SessionID)
			require.Equal(t, tc.url, e.URL)
			client := snap.OrderID
			if tc.client != "" {
				client = tc.client
			}
			operation := snap.OperationID
			if tc.operation != "" {
				operation = tc.operation
			}
			require.Equal(t, client, e.ClientReferenceID)
			require.Equal(t, map[string]string{"order_id": snap.OrderID, "operation_id": operation}, e.Metadata)
			require.Equal(t, "payment", e.Mode)
			require.Equal(t, snap.ExpiresAt, e.ExpiresAt)
			if tc.intent == "" {
				require.Nil(t, e.PaymentIntentID)
			}
			if tc.intent != "" {
				require.NotNil(t, e.PaymentIntentID)
				require.Equal(t, tc.intent, *e.PaymentIntentID)
			}
			require.Equal(t, "req_test_checkout", e.RequestID)
			require.Len(t, server.Wires(), 1)
			if tc.session != "" {
				retrieved, err := gateway.Retrieve(context.Background(), tc.session)
				require.NoError(t, err)
				require.Equal(t, e.SessionID, retrieved.SessionID)
				expected := e
				expected.ObservedAt = retrieved.ObservedAt
				require.Equal(t, expected, retrieved, "retrieval must translate full wire evidence rather than use cached correlation")
				require.Equal(t, "GET", server.Wires()[1].Method)
				require.Empty(t, server.Wires()[1].Header.Get("Idempotency-Key"))
			}
		})
	}
}

func TestSDKFullWireEvidenceFidelity(t *testing.T) { // LIFE-002 LIFE-007 STR-008
	for _, tc := range []struct {
		name, omit                     string
		mode, client, order, operation string
		expiry                         int64
		intent                         *string
	}{
		{"all_fields", "", "payment", "remote_client", "remote_order", "remote_operation", 1799999999, testutil.Pointer("pi_wire")},
		{"missing_client", "client_reference_id", "payment", "", "remote_order", "remote_operation", 1799999999, nil},
		{"missing_mode", "mode", "", "remote_client", "remote_order", "remote_operation", 1799999999, nil},
		{"wrong_mode", "", "subscription", "remote_client", "remote_order", "remote_operation", 1799999999, nil},
		{"missing_metadata", "metadata", "payment", "remote_client", "", "", 1799999999, nil},
		{"missing_order_metadata", "metadata.order_id", "payment", "remote_client", "", "remote_operation", 1799999999, nil},
		{"missing_operation_metadata", "metadata.operation_id", "payment", "remote_client", "remote_order", "", 1799999999, nil},
		{"missing_session", "id", "payment", "remote_client", "remote_order", "remote_operation", 1799999999, nil},
		{"missing_expiry", "expires_at", "payment", "remote_client", "remote_order", "remote_operation", 0, nil},
		{"wrong_expiry", "", "payment", "remote_client", "remote_order", "remote_operation", 42, nil},
		{"missing_intent", "payment_intent", "payment", "remote_client", "remote_order", "remote_operation", 1799999999, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := payment.SessionEvidence{SessionID: "cs_test_wire", ClientReferenceID: tc.client, Metadata: map[string]string{"order_id": tc.order, "operation_id": tc.operation}, AmountTotal: 777, Currency: "eur", Mode: tc.mode, Livemode: true, Status: "complete", PaymentStatus: "paid", PaymentIntentID: tc.intent, URL: "https://checkout.stripe.com/c/pay/wire", ExpiresAt: tc.expiry, RequestID: "req_wire"}
			obj := testutil.SessionJSON(want)
			switch tc.omit {
			case "metadata.order_id":
				delete(want.Metadata, "order_id")
			case "metadata.operation_id":
				delete(want.Metadata, "operation_id")
			case "metadata":
				delete(obj, "metadata")
				want.Metadata = nil
			case "id":
				delete(obj, "id")
				want.SessionID = ""
			default:
				if tc.omit != "" {
					delete(obj, tc.omit)
				}
			}
			body, err := json.Marshal(obj)
			require.NoError(t, err)
			wires := make(chan testutil.Wire, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wires <- testutil.Wire{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Request-Id", "req_wire")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			g := New("sk_test_fixture", Options{BackendURL: server.URL, HTTPClient: server.Client()})
			require.NotNil(t, g)
			start := time.Now()
			created, err := g.Create(context.Background(), testutil.Intent().Operation.Snapshot)
			require.NoError(t, err)
			assertSDKWireEvidence(t, want, created, start)
			start = time.Now()
			retrieved, err := g.Retrieve(context.Background(), "cs_test_requested")
			require.NoError(t, err)
			assertSDKWireEvidence(t, want, retrieved, start)
			first := <-wires
			second := <-wires
			require.Equal(t, "POST", first.Method)
			require.Equal(t, "GET", second.Method)
			require.Equal(t, "/v1/checkout/sessions/cs_test_requested", second.Path)
			require.Empty(t, second.Header.Get("Idempotency-Key"))
		})
	}
}
func assertSDKWireEvidence(t *testing.T, want, got payment.SessionEvidence, start time.Time) {
	t.Helper()
	require.False(t, got.ObservedAt.Before(start))
	require.False(t, got.ObservedAt.After(time.Now()))
	want.ObservedAt = got.ObservedAt
	require.Equal(t, want, got)
}
