package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCheckoutServingValidation(t *testing.T) { // CFG-001 CFG-002 CFG-003 FND-002
	databaseURL, _ := commandDatabase(t)
	for _, tc := range []struct{ name, key, value string }{{"live_key", "STRIPE_SECRET_KEY", "sk_live_sensitive_fixture"}, {"missing_key", "STRIPE_SECRET_KEY", ""}, {"empty_suffix", "STRIPE_SECRET_KEY", "sk_test_"}, {"remote_origin", "APP_BASE_URL", "http://example.com"}, {"https_origin", "APP_BASE_URL", "https://localhost"}, {"currency", "PAYMENT_CURRENCY", "eur"}, {"amount_limits", "PAYMENT_MIN_AMOUNT", "100001"}, {"request_budget", "CHECKOUT_REQUEST_TIMEOUT", "11s"}, {"retry_budget", "STRIPE_RETRY_BUDGET", "8s"}, {"attempts", "STRIPE_MAX_ATTEMPTS", "4"}} {
		t.Run(tc.name, func(t *testing.T) {
			values := commandEnvironment(t, databaseURL)
			values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://localhost:8080"
			values["DB_STARTUP_TIMEOUT"] = "5s"
			// A real valid-config control establishes that the same migrated database
			// and command environment can serve; dependency failure is not rejection.
			address, stop := startCommand(t, &commandEnv{values: values})
			response, err := http.Get(address + "/readyz")
			require.NoError(t, err)
			require.Equal(t, 200, response.StatusCode)
			require.NoError(t, response.Body.Close())
			stop()
			values["LISTEN_ADDR"] = commandAddress(t)
			values[tc.key] = tc.value
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			logs := &commandOutput{}
			done := make(chan error, 1)
			go func() { done <- run(ctx, []string{"serve"}, func(k string) string { return values[k] }, logs) }()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			accepted := false
			var runErr error
		observe:
			for {
				select {
				case runErr = <-done:
					break observe
				case <-ticker.C:
					conn, e := net.DialTimeout("tcp", values["LISTEN_ADDR"], 20*time.Millisecond)
					if e == nil {
						accepted = true
						_ = conn.Close()
					}
				case <-ctx.Done():
					runErr = <-done
					break observe
				}
			}
			require.Nil(t, ctx.Err(), "hang guard/deadline is not normal configuration rejection")
			require.False(t, accepted, "invalid configuration must never open a listener")
			require.Error(t, runErr, "invalid checkout setting must reject serving normally")
			require.False(t, errors.Is(runErr, context.DeadlineExceeded), "database startup deadline is not configuration rejection")
			require.False(t, errors.Is(runErr, context.Canceled), "cancellation is not configuration rejection")
			require.NotContains(t, logs.contents(), "service listening")
			require.NotContains(t, logs.contents(), "sensitive_fixture")
		})
	}
}

type checkoutRedirectTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (tr checkoutRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	if r.URL.Host == "api.stripe.com" {
		u := *r.URL
		u.Scheme = tr.target.Scheme
		u.Host = tr.target.Host
		copy.URL = &u
		copy.Host = tr.target.Host
	}
	return tr.next.RoundTrip(copy)
}
func TestCheckoutCommandWiring(t *testing.T) { // HTTP-001 HTTP-002 HTTP-005 SEC-001 FND-002 CFG-001 CFG-002 CFG-003
	dbURL, pool := commandDatabase(t)
	s := testutil.NewStripeServer(t)
	target, err := url.Parse(s.Server.URL)
	require.NoError(t, err)
	prior := http.DefaultTransport
	http.DefaultTransport = checkoutRedirectTransport{target, prior}
	defer func() { http.DefaultTransport = prior }()
	values := commandEnvironment(t, dbURL)
	values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://127.0.0.1:9099/"
	values["PAYMENT_MIN_AMOUNT"], values["PAYMENT_MAX_AMOUNT"] = "500", "1000"
	values["CHECKOUT_REQUEST_TIMEOUT"], values["STRIPE_CALL_TIMEOUT"], values["STRIPE_RETRY_BUDGET"], values["STRIPE_MAX_ATTEMPTS"] = "2s", "100ms", "500ms", "1"
	values["HTTP_READ_TIMEOUT"], values["HTTP_WRITE_TIMEOUT"] = "3s", "3s"
	address, stop := startCommand(t, &commandEnv{values: values})
	defer stop()
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(address + path)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	request, err := http.NewRequest("POST", address+"/api/orders", strings.NewReader(`{"description":"ÉCOLE  Café e\u0301","amount":500,"request_key":"11111111-1111-4111-8111-111111111111"}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(commandUser, commandPassword)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { assert.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, 201, response.StatusCode, "actual command must wire payment DB and SDK into protected API")
	require.Contains(t, string(body), "checkout.stripe.com")
	require.NotContains(t, string(body), commandPassword)
	require.NotContains(t, string(body), "sk_test_fixture")
	require.Len(t, s.Wires(), 1)
	require.Equal(t, "Bearer sk_test_fixture", s.Wires()[0].Header.Get("Authorization"))
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(body, &envelope))
	order := envelope["order"].(map[string]any)
	id := order["id"].(string)
	require.Equal(t, "ÉCOLE  Café e\u0301", order["description"])
	require.Equal(t, float64(500), order["amount"])
	require.Equal(t, "ÉCOLE  Café e\u0301", s.Wires()[0].Form.Get("line_items[0][price_data][product_data][name]"))
	require.Equal(t, "500", s.Wires()[0].Form.Get("line_items[0][price_data][unit_amount]"))
	for field, returnKind := range map[string]string{"success_url": "success", "cancel_url": "cancel"} {
		u, err := url.Parse(s.Wires()[0].Form.Get(field))
		require.NoError(t, err)
		require.Equal(t, "http", u.Scheme)
		require.Equal(t, "127.0.0.1:9099", u.Host)
		require.Equal(t, "/", u.Path)
		require.Equal(t, url.Values{"order_id": []string{id}, "checkout_return": []string{returnKind}}, u.Query())
	}
	for _, tc := range []struct{ name, method, path, body string }{
		{"valid_checkout", "POST", "/api/orders/" + id + "/checkout", fmt.Sprintf(`{"request_key":%q}`, uuid.NewString())},
		{"valid_order_get", "GET", "/api/orders/" + id, ""}, {"valid_order_head", "HEAD", "/api/orders/" + id, ""}, {"valid_history_get", "GET", "/api/orders/" + id + "/history", ""}, {"valid_history_head", "HEAD", "/api/orders/" + id + "/history", ""},
	} {
		{
			status, data, _ := commandCheckoutRequest(t, address, tc.method, tc.path, tc.body, "valid")
			require.Equal(t, 200, status)
			if tc.method == "HEAD" {
				require.Empty(t, data)
			}
		}
	}
	before, err := testutil.DurableRows(context.Background(), pool)
	require.NoError(t, err)
	wireCount := len(s.Wires())
	for _, tc := range []struct{ name, method, path string }{
		{"auth_initial", "POST", "/api/orders"}, {"auth_checkout", "POST", "/api/orders/" + id + "/checkout"}, {"auth_order_get", "GET", "/api/orders/" + id}, {"auth_order_head", "HEAD", "/api/orders/" + id}, {"auth_history_get", "GET", "/api/orders/" + id + "/history"}, {"auth_history_head", "HEAD", "/api/orders/" + id + "/history"},
	} {
		{
			for _, credential := range []struct{ name string }{{"missing"}, {"wrong"}} {
				{
					status, data, headers := commandCheckoutRequest(t, address, tc.method, tc.path, `{broken`, credential.name)
					require.Equal(t, 401, status)
					require.NotEmpty(t, headers.Get("WWW-Authenticate"))
					require.NotContains(t, string(data), commandPassword)
					after, err := testutil.DurableRows(context.Background(), pool)
					require.NoError(t, err)
					require.Equal(t, before, after)
					require.Len(t, s.Wires(), wireCount)
				}
			}
		}
	}
	for _, tc := range []struct {
		name   string
		amount int
		status int
	}{{"below_custom_min", 499, 400}, {"above_custom_max", 1001, 400}, {"custom_max", 1000, 201}} {
		{
			status, data, _ := commandCheckoutRequest(t, address, "POST", "/api/orders", fmt.Sprintf(`{"description":"custom boundary","amount":%d,"request_key":%q}`, tc.amount, uuid.NewString()), "valid")
			require.Equal(t, tc.status, status)
			if tc.status == 201 {
				var got map[string]any
				require.NoError(t, json.Unmarshal(data, &got))
				require.Equal(t, float64(1000), got["order"].(map[string]any)["amount"])
				require.Equal(t, "1000", s.Wires()[len(s.Wires())-1].Form.Get("line_items[0][price_data][unit_amount]"))
			} else {
				after, err := testutil.DurableRows(context.Background(), pool)
				require.NoError(t, err)
				require.Equal(t, before, after)
				require.Len(t, s.Wires(), wireCount)
			}
		}
	}

}

func commandCheckoutRequest(t *testing.T, address, method, path, body, credential string) (int, []byte, http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, address+path, strings.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	if credential == "valid" {
		r.SetBasicAuth(commandUser, commandPassword)
	} else if credential == "wrong" {
		r.SetBasicAuth(commandUser, "wrong")
	}
	response, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	defer func() { assert.NoError(t, response.Body.Close()) }()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, data, response.Header
}

func TestCheckoutCustomCallBudgetAndAttempts(t *testing.T) { // CFG-003 STR-004 STR-005 FND-002
	dbURL, _ := commandDatabase(t)
	s := testutil.NewStripeServer(t)
	barrier := testutil.NewBarrier()
	s.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
	})
	target, err := url.Parse(s.Server.URL)
	require.NoError(t, err)
	prior := http.DefaultTransport
	http.DefaultTransport = checkoutRedirectTransport{target, prior}
	defer func() { http.DefaultTransport = prior }()
	values := commandEnvironment(t, dbURL)
	values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://localhost:8080"
	values["CHECKOUT_REQUEST_TIMEOUT"], values["STRIPE_CALL_TIMEOUT"], values["STRIPE_RETRY_BUDGET"], values["STRIPE_MAX_ATTEMPTS"] = "2s", "100ms", "500ms", "1"
	values["HTTP_READ_TIMEOUT"], values["HTTP_WRITE_TIMEOUT"] = "3s", "3s"
	address, stop := startCommand(t, &commandEnv{values: values})
	defer stop()
	start := time.Now()
	status, body, _ := commandCheckoutRequest(t, address, "POST", "/api/orders", fmt.Sprintf(`{"description":"custom call budget","amount":2500,"request_key":%q}`, uuid.NewString()), "valid")
	require.Equal(t, 202, status)
	require.Less(t, time.Since(start), time.Second, "custom call deadline must reach real SDK serving operations")
	require.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	select {
	case <-barrier.Done:
	case <-time.After(time.Second):
		t.Fatal("timed out external work did not join")
	}
	require.Len(t, s.Wires(), 1, "one configured attempt must suppress default retries")
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Equal(t, "unresolved", envelope["operation"].(map[string]any)["state"])
	require.Equal(t, "unpaid", envelope["order"].(map[string]any)["payment_status"])
}
