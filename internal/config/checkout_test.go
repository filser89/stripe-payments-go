package config_test

import (
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strconv"
	"strings"
	"testing"
	"time"
)

func checkoutConfiguration(t *testing.T, changes map[string]string) (config.Config, error) {
	t.Helper()
	env := testutil.Environment("postgres://fixture:fixture@localhost:5432/fixture", "127.0.0.1:8080")
	for k, v := range changes {
		env[k] = v
	}
	return config.Load(func(k string) string { return env[k] })
}
func TestCheckoutAmountConfiguration(t *testing.T) { // CFG-001
	c, err := checkoutConfiguration(t, nil)
	require.NoError(t, err)
	s := c.CheckoutSettings()
	assert.Equal(t, "usd", s.Currency)
	assert.EqualValues(t, 50, s.MinAmount)
	assert.EqualValues(t, 100000, s.MaxAmount)
	for _, tc := range []struct{ min, max string }{{"50", "50"}, {"50", "100000"}, {"100000", "100000"}, {"500", "1000"}} {
		c, e := checkoutConfiguration(t, map[string]string{"PAYMENT_MIN_AMOUNT": tc.min, "PAYMENT_MAX_AMOUNT": tc.max})
		assert.NoError(t, e)
		assert.NoError(t, c.ValidateServing())
		min, err := strconv.ParseInt(tc.min, 10, 64)
		require.NoError(t, err)
		max, err := strconv.ParseInt(tc.max, 10, 64)
		require.NoError(t, err)
		assert.Equal(t, min, c.CheckoutSettings().MinAmount)
		assert.Equal(t, max, c.CheckoutSettings().MaxAmount)
		assert.Equal(t, "usd", c.CheckoutSettings().Currency)
	}
	c, e := checkoutConfiguration(t, map[string]string{"PAYMENT_MIN_AMOUNT": "1000", "PAYMENT_MAX_AMOUNT": "500"})
	if e == nil {
		e = c.ValidateServing()
	}
	require.Error(t, e, "reversed limits")
	for _, tc := range []struct{ key, value string }{{"PAYMENT_CURRENCY", "USD"}, {"PAYMENT_CURRENCY", "eur"}, {"PAYMENT_MIN_AMOUNT", "49"}, {"PAYMENT_MAX_AMOUNT", "100001"}, {"PAYMENT_MIN_AMOUNT", "0x32"}, {"PAYMENT_MAX_AMOUNT", "1e5"}, {"PAYMENT_MIN_AMOUNT", "50.0"}, {"PAYMENT_MAX_AMOUNT", "9223372036854775808"}, {"PAYMENT_MIN_AMOUNT", " 50"}, {"PAYMENT_MIN_AMOUNT", "100001"}, {"PAYMENT_MAX_AMOUNT", "49"}} {
		c, e := checkoutConfiguration(t, map[string]string{tc.key: tc.value})
		if e == nil {
			e = c.ValidateServing()
		}
		assert.Error(t, e, tc.key+"="+tc.value)
		if e != nil {
			assert.NotContains(t, e.Error(), tc.value)
		}
	}
}
func TestCheckoutSandboxOriginConfiguration(t *testing.T) { // CFG-002
	c, err := checkoutConfiguration(t, nil)
	require.NoError(t, err)
	assert.NoError(t, c.ValidateServing())
	assert.Equal(t, "sk_test_fixture", c.CheckoutSettings().SecretKey)
	assert.Equal(t, "http://localhost:8080", c.CheckoutSettings().BaseURL)
	for _, origin := range []string{"http://localhost", "http://localhost/", "http://127.0.0.1:8080", "http://[::1]", "http://[::1]:65535/"} {
		c, e := checkoutConfiguration(t, map[string]string{"APP_BASE_URL": origin})
		assert.NoError(t, e)
		assert.NoError(t, c.ValidateServing())
		assert.Equal(t, strings.TrimSuffix(origin, "/"), strings.TrimSuffix(c.CheckoutSettings().BaseURL, "/"), "effective origin must preserve host and port")
	}
	for _, key := range []string{"", "sk_test_", "sk_live_fixture", "rk_test_fixture", " sk_test_fixture", "sk_test_fixture ", "sk_test_\u0085"} {
		c, e := checkoutConfiguration(t, map[string]string{"STRIPE_SECRET_KEY": key})
		if e == nil {
			e = c.ValidateServing()
		}
		assert.Error(t, e, "sandbox key class")
	}
	for _, origin := range []string{"https://localhost", "/relative", "http:///", "http://example.com", "http://localhost:0", "http://localhost:65536", "http://u:p@localhost", "http://localhost/path", "http://localhost?query=1", "http://localhost#fragment"} {
		c, e := checkoutConfiguration(t, map[string]string{"APP_BASE_URL": origin})
		if e == nil {
			e = c.ValidateServing()
		}
		assert.Error(t, e, origin)
	}
	missing, missingErr := checkoutConfiguration(t, map[string]string{"APP_BASE_URL": ""})
	require.NoError(t, missingErr)
	require.NoError(t, missing.ValidateServing())
	require.Equal(t, "http://localhost:8080", missing.CheckoutSettings().BaseURL)
	custom, customErr := checkoutConfiguration(t, map[string]string{"STRIPE_SECRET_KEY": "sk_test_custom_fixture"})
	require.NoError(t, customErr)
	require.NoError(t, custom.ValidateServing())
	require.Equal(t, "sk_test_custom_fixture", custom.CheckoutSettings().SecretKey)
	env := testutil.Environment("postgres://fixture:fixture@localhost/fixture", "localhost:8080")
	captured, e := config.Load(func(k string) string { return env[k] })
	require.NoError(t, e)
	env["STRIPE_SECRET_KEY"] = "sk_live_changed"
	env["APP_BASE_URL"] = "http://example.com"
	assert.Equal(t, "sk_test_fixture", captured.CheckoutSettings().SecretKey)
	assert.Equal(t, "http://localhost:8080", captured.CheckoutSettings().BaseURL)
}
func TestCheckoutBudgetConfiguration(t *testing.T) { // CFG-003
	c, err := checkoutConfiguration(t, nil)
	require.NoError(t, err)
	s := c.CheckoutSettings()
	assert.Equal(t, 10*time.Second, s.RequestTimeout)
	assert.Equal(t, 2*time.Second, s.CallTimeout)
	assert.Equal(t, 7*time.Second, s.RetryBudget)
	assert.Equal(t, 3, s.MaxAttempts)
	assert.Equal(t, 11*time.Second, c.ReadTimeout)
	assert.Equal(t, 15*time.Second, c.WriteTimeout)
	assert.Equal(t, 10*time.Second, c.ShutdownGrace)
	assert.Equal(t, 5*time.Second, c.CleanupTimeout)
	for _, tc := range []struct{ key, value string }{{"CHECKOUT_REQUEST_TIMEOUT", "2s"}, {"CHECKOUT_REQUEST_TIMEOUT", "10s"}, {"STRIPE_CALL_TIMEOUT", "100ms"}, {"STRIPE_CALL_TIMEOUT", "2s"}, {"STRIPE_RETRY_BUDGET", "500ms"}, {"STRIPE_RETRY_BUDGET", "7s"}, {"STRIPE_MAX_ATTEMPTS", "1"}, {"STRIPE_MAX_ATTEMPTS", "3"}} {
		changes := map[string]string{tc.key: tc.value}
		if tc.value == "2s" && tc.key == "CHECKOUT_REQUEST_TIMEOUT" {
			changes["STRIPE_RETRY_BUDGET"] = "1s"
			changes["STRIPE_CALL_TIMEOUT"] = "100ms"
		}
		if tc.value == "500ms" {
			changes["STRIPE_CALL_TIMEOUT"] = "100ms"
		}
		c, e := checkoutConfiguration(t, changes)
		assert.NoError(t, e)
		assert.NoError(t, c.ValidateServing())
		expected := s
		for key, value := range changes {
			switch key {
			case "CHECKOUT_REQUEST_TIMEOUT":
				expected.RequestTimeout, e = time.ParseDuration(value)
			case "STRIPE_CALL_TIMEOUT":
				expected.CallTimeout, e = time.ParseDuration(value)
			case "STRIPE_RETRY_BUDGET":
				expected.RetryBudget, e = time.ParseDuration(value)
			case "STRIPE_MAX_ATTEMPTS":
				expected.MaxAttempts, e = strconv.Atoi(value)
			}
			require.NoError(t, e)
		}
		assert.Equal(t, expected, c.CheckoutSettings())
	}
	for _, tc := range []struct{ key, value string }{{"CHECKOUT_REQUEST_TIMEOUT", "1.999s"}, {"CHECKOUT_REQUEST_TIMEOUT", "10.001s"}, {"CHECKOUT_REQUEST_TIMEOUT", "invalid"}, {"STRIPE_CALL_TIMEOUT", "99ms"}, {"STRIPE_CALL_TIMEOUT", "2001ms"}, {"STRIPE_CALL_TIMEOUT", "invalid"}, {"STRIPE_RETRY_BUDGET", "499ms"}, {"STRIPE_RETRY_BUDGET", "7001ms"}, {"STRIPE_RETRY_BUDGET", "invalid"}, {"STRIPE_MAX_ATTEMPTS", "0"}, {"STRIPE_MAX_ATTEMPTS", "4"}, {"STRIPE_MAX_ATTEMPTS", "1.5"}, {"HTTP_READ_TIMEOUT", "10s"}, {"HTTP_WRITE_TIMEOUT", "10s"}} {
		c, e := checkoutConfiguration(t, map[string]string{tc.key: tc.value})
		if e == nil {
			e = c.ValidateServing()
		}
		assert.Error(t, e, tc.key+tc.value)
		if e != nil {
			assert.NotContains(t, e.Error(), "sk_test_fixture")
		}
	}
	for _, changes := range []map[string]string{{"CHECKOUT_REQUEST_TIMEOUT": "2s", "STRIPE_RETRY_BUDGET": "2s", "STRIPE_CALL_TIMEOUT": "100ms"}, {"STRIPE_RETRY_BUDGET": "500ms", "STRIPE_CALL_TIMEOUT": "1s"}} {
		c, e := checkoutConfiguration(t, changes)
		if e == nil {
			e = c.ValidateServing()
		}
		assert.Error(t, e)
	}
	c, e := checkoutConfiguration(t, map[string]string{"HTTP_READ_TIMEOUT": "11s", "HTTP_WRITE_TIMEOUT": "11s"})
	assert.NoError(t, e)
	assert.NoError(t, c.ValidateServing())
	assert.Equal(t, 11*time.Second, c.CheckoutSettings().RequestTimeout+time.Second)
}
