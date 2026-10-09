package main

import (
	"context"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/testutil"
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
func TestCheckoutCommandWiring(t *testing.T) { // HTTP-001 HTTP-005 FND-002 CFG-002
	dbURL, _ := commandDatabase(t)
	s := testutil.NewStripeServer(t)
	target, err := url.Parse(s.Server.URL)
	require.NoError(t, err)
	prior := http.DefaultTransport
	http.DefaultTransport = checkoutRedirectTransport{target, prior}
	defer func() { http.DefaultTransport = prior }()
	values := commandEnvironment(t, dbURL)
	values["STRIPE_SECRET_KEY"], values["APP_BASE_URL"] = "sk_test_fixture", "http://localhost:8080"
	address, stop := startCommand(t, &commandEnv{values: values})
	defer stop()
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(address + path)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	request, err := http.NewRequest("POST", address+"/api/orders", strings.NewReader(`{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`))
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
}
