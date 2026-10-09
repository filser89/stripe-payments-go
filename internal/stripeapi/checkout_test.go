package stripeapi

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v87"
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
				require.Equal(t, "GET", server.Wires()[1].Method)
				require.Empty(t, server.Wires()[1].Header.Get("Idempotency-Key"))
			}
		})
	}
}
