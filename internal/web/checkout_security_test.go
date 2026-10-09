package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"strings"
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
				if record["order_id"] == f.Outcome.Order.ID && record["operation_id"] == f.Outcome.Operation.ID && record["request_id"] == w.Header().Get("X-Request-ID") && checkoutLogContext(record, tc.code, f.Outcome.Operation.State) {
					feature = true
				}
			}
			require.True(t, feature, "feature outcome must log known durable IDs and response request correlation")
		})
	}
}

func checkoutLogContext(record map[string]any, code, state string) bool {
	// The field names are implementation choices; semantic values must describe
	// the actual outcome in addition to IDs and generic request-completion data.
	relevant := map[string]any{}
	for key, value := range record {
		switch key {
		case "order_id", "operation_id", "request_id", "time", "level", "duration_ms", "status":
			continue
		}
		relevant[key] = value
	}
	body, _ := json.Marshal(relevant)
	text := strings.ToLower(string(body))
	if code != "" {
		if strings.Contains(text, code) {
			return true
		}
		terms := map[string][]string{"checkout_rejected": {"reject"}, "temporarily_unavailable": {"unavailable", "database", "deadline", "timeout", "cancel", "server", "mismatch", "ownership"}, "idempotency_conflict": {"conflict"}}
		for _, term := range terms[code] {
			if strings.Contains(text, term) {
				return true
			}
		}
		return false
	}
	if strings.Contains(text, state) {
		return true
	}
	if state == "open" {
		return strings.Contains(text, "success") || strings.Contains(text, "accepted") || strings.Contains(text, "established")
	}
	if state == "prepared" || state == "unresolved" {
		return strings.Contains(text, "pending") || strings.Contains(text, "accepted")
	}
	return false
}
func TestCheckoutAuthenticationMethodMatrix(t *testing.T) { // SEC-001
	for _, tc := range []struct{ name, method, suffix, body string }{
		{"initial", "POST", "", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`},
		{"checkout", "POST", "/11111111-1111-4111-8111-111111111111/checkout", `{"request_key":"11111111-1111-4111-8111-111111111111"}`},
		{"order_get", "GET", "/11111111-1111-4111-8111-111111111111", ""},
		{"order_head", "HEAD", "/11111111-1111-4111-8111-111111111111", ""},
		{"history_get", "GET", "/11111111-1111-4111-8111-111111111111/history", ""},
		{"history_head", "HEAD", "/11111111-1111-4111-8111-111111111111/history", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, credential := range []struct {
				name         string
				valid, wrong bool
			}{{"valid", true, false}, {"missing", false, false}, {"wrong", false, true}} {
				t.Run(credential.name, func(t *testing.T) {
					f := preparedCheckout()
					h, _ := checkoutHandler(t, f, 2*time.Second)
					r := testutil.Request(tc.method, "/api/orders"+tc.suffix, tc.body, credential.valid)
					if credential.wrong {
						r.SetBasicAuth("checkout-fixture-user", "wrong")
					}
					reader := &checkoutReadFault{Data: []byte(tc.body), Error: io.EOF}
					r.Body = reader
					r.ContentLength = -1
					w := testutil.Response(t, h, r)
					if credential.valid {
						require.Less(t, w.Code, 400)
						require.EqualValues(t, 1, f.Effects())
					} else {
						require.Equal(t, 401, w.Code)
						require.Zero(t, reader.Reads.Load())
						require.Zero(t, f.Effects())
					}
					if tc.method == "HEAD" {
						require.Empty(t, w.Body.String())
					}
				})
			}
		})
	}
}
func TestCheckoutSequentialCredentialIsolation(t *testing.T) { // SEC-001
	f := preparedCheckout()
	h, _ := checkoutHandler(t, f, 2*time.Second)
	valid := testutil.Response(t, h, testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true))
	require.Equal(t, 201, valid.Code)
	effects := f.Effects()
	for _, tc := range []struct{ name, method, path string }{
		{"initial", "POST", "/api/orders"}, {"checkout", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/checkout"}, {"order_get", "GET", "/api/orders/11111111-1111-4111-8111-111111111111"}, {"order_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111"}, {"history_get", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history"}, {"history_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history"},
	} {
		{
			r := testutil.Request(tc.method, tc.path, "", false)
			reader := &checkoutReadFault{Error: errors.New("read-sentinel")}
			r.Body = reader
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			require.Equal(t, 401, w.Code)
			require.Zero(t, reader.Reads.Load())
			require.Equal(t, effects, f.Effects())
		}
	}
}
func TestCheckoutOverlappingCredentialIsolation(t *testing.T) { // SEC-001
	f := preparedCheckout()
	barrier := testutil.NewBarrier()
	f.Before = func(ctx context.Context) { _ = barrier.Wait(ctx) }
	h, _ := checkoutHandler(t, f, 2*time.Second)
	valid := testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true)
	ctx, cancel := context.WithCancel(valid.Context())
	defer cancel()
	valid = valid.WithContext(ctx)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, valid); done <- w }()
	select {
	case <-barrier.Arrived:
	case <-time.After(time.Second):
		t.Fatal("authenticated operation never entered")
	}
	defer barrier.Release()
	for _, tc := range []struct{ name, method, path string }{
		{"initial", "POST", "/api/orders"}, {"checkout", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/checkout"}, {"order_get", "GET", "/api/orders/11111111-1111-4111-8111-111111111111"}, {"order_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111"}, {"history_get", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history"}, {"history_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history"},
	} {
		{
			r := testutil.Request(tc.method, tc.path, "", false)
			r.SetBasicAuth("checkout-fixture-user", "wrong")
			reader := &checkoutReadFault{Error: errors.New("read-sentinel")}
			r.Body = reader
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			require.Equal(t, 401, w.Code)
			require.Zero(t, reader.Reads.Load())
			require.EqualValues(t, 1, f.Effects())
		}
	}
	barrier.Release()
	select {
	case w := <-done:
		require.Equal(t, 201, w.Code)
	case <-time.After(time.Second):
		t.Fatal("authenticated handler did not join")
	}
	require.EqualValues(t, 1, f.Created.Load())
	require.Zero(t, f.Continued.Load())
	require.Zero(t, f.Read.Load())
	require.Zero(t, f.Histories.Load())
}
func TestCheckoutPendingFailureLogContext(t *testing.T) { // SEC-002 SEC-003
	for _, tc := range []struct {
		name, state, code string
		cause             error
	}{
		{"prepared", "prepared", "", nil}, {"unresolved", "unresolved", "", nil},
		{"result_commit", "unresolved", "temporarily_unavailable", errors.New("sensitive-sentinel result commit")},
		{"read_failure", "unresolved", "temporarily_unavailable", errors.New("sensitive-sentinel read unavailable")},
		{"timeout", "unresolved", "temporarily_unavailable", context.DeadlineExceeded},
		{"cancellation", "unresolved", "temporarily_unavailable", context.Canceled},
		{"server", "unresolved", "temporarily_unavailable", errors.New("sensitive-sentinel server failure")},
		{"mismatch", "unresolved", "temporarily_unavailable", errors.New("sensitive-sentinel correlation mismatch")},
		{"ownership_loss", "unresolved", "temporarily_unavailable", errors.New("sensitive-sentinel ownership loss")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			f.Outcome.Operation.State = tc.state
			f.Outcome.Established = false
			f.Outcome.Pending = true
			f.Outcome.Operation.SessionID = nil
			f.Outcome.Operation.CheckoutURL = nil
			if tc.code != "" {
				f.Err = &payment.Error{Code: tc.code, OrderID: f.Outcome.Order.ID, OperationID: f.Outcome.Operation.ID, Cause: tc.cause}
			}
			h, logs := checkoutHandler(t, f, 2*time.Second)
			w := testutil.Response(t, h, testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true))
			if tc.code == "" {
				require.Equal(t, 202, w.Code)
			} else {
				require.Equal(t, 503, w.Code)
			}
			records, err := logs.Records()
			require.NoError(t, err)
			found := false
			for _, record := range records {
				if record["request_id"] == w.Header().Get("X-Request-ID") && record["order_id"] == f.Outcome.Order.ID && record["operation_id"] == f.Outcome.Operation.ID && checkoutLogContext(record, tc.code, tc.state) {
					found = true
				}
			}
			require.True(t, found, "correlated feature record must carry outcome or failure category")
			require.NotContains(t, logs.Contents()+w.Body.String(), "sensitive-sentinel")
		})
	}
}
