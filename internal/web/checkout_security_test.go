package web

import (
	"encoding/base64"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCheckoutEndpointAuthentication(t *testing.T) { // SEC-001
	for _, tc := range []struct{ name, method, path, credential string }{{"initial_missing", "POST", "/api/orders", ""}, {"checkout_wrong", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/checkout", "wrong"}, {"order_missing", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", ""}, {"history_wrong", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history", "wrong"}, {"unknown_protected", "PATCH", "/api/unknown", ""}, {"cookie", "POST", "/api/orders", "cookie"}, {"query", "GET", "/api/orders?username=checkout-fixture-user&password=checkout-fixture-password", ""}, {"body_credentials", "POST", "/api/orders", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, time.Second)
			r := testutil.Request(tc.method, tc.path, "", false)
			reader := &checkoutReadFault{Data: []byte(`{"username":"checkout-fixture-user","password":"checkout-fixture-password"}`), Error: errors.New("read-sentinel")}
			r.Body = reader
			r.ContentLength = -1
			if tc.credential == "wrong" {
				r.SetBasicAuth("checkout-fixture-user", "wrong")
			}
			if tc.credential == "cookie" {
				r.Header.Set("Cookie", "username=checkout-fixture-user; password=checkout-fixture-password")
			}
			w := testutil.Response(t, h, r)
			require.Equal(t, 401, w.Code)
			require.Zero(t, reader.Reads.Load(), "authentication must precede body parsing")
			require.Zero(t, f.Effects())
			require.NotContains(t, w.Body.String(), "checkout-fixture-password")
		})
	}
}
func TestCheckoutSanitizedLogsAndResponses(t *testing.T) { // SEC-002 SEC-003
	for _, tc := range []struct{ name, code string }{{"success", ""}, {"rejected", "checkout_rejected"}, {"database", "temporarily_unavailable"}, {"conflict", "idempotency_conflict"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			if tc.code != "" {
				f.Err = checkoutError(tc.code, f)
			}
			h, logs := checkoutHandler(t, f, time.Second)
			r := testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true)
			w := testutil.Response(t, h, r)
			records, err := logs.Records()
			require.NoError(t, err)
			require.NotEmpty(t, records)
			joined := logs.Contents() + w.Body.String()
			for _, sentinel := range []string{"checkout-fixture-password", "sk_test_fixture", "test-only-fixture", "sensitive-sentinel", "4242424242424242", "123-cvc", base64.StdEncoding.EncodeToString([]byte("checkout-fixture-user:checkout-fixture-password"))} {
				require.NotContains(t, joined, sentinel)
			}
			var feature bool
			for _, record := range records {
				if record["order_id"] == f.Outcome.Order.ID && record["operation_id"] == f.Outcome.Operation.ID && record["request_id"] == w.Header().Get("X-Request-ID") {
					feature = true
				}
			}
			require.True(t, feature, "feature outcome must log known durable IDs and response request correlation")
		})
	}
}
