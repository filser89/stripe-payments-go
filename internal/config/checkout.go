package config

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CheckoutConfig struct {
	SecretKey, BaseURL, Currency             string
	MinAmount, MaxAmount                     int64
	RequestTimeout, CallTimeout, RetryBudget time.Duration
	MaxAttempts                              int
}

func (c Config) CheckoutSettings() CheckoutConfig { return c.checkout }
func captureCheckout(getenv func(string) string) (CheckoutConfig, error) {
	value := func(k, f string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return f
	}
	s := CheckoutConfig{SecretKey: getenv("STRIPE_SECRET_KEY"), BaseURL: value("APP_BASE_URL", "http://localhost:8080"), Currency: value("PAYMENT_CURRENCY", "usd")}
	for _, f := range []struct {
		k, d string
		p    *int64
	}{{"PAYMENT_MIN_AMOUNT", "50", &s.MinAmount}, {"PAYMENT_MAX_AMOUNT", "100000", &s.MaxAmount}} {
		raw := value(f.k, f.d)
		for _, r := range raw {
			if r < '0' || r > '9' {
				return s, errors.New(f.k + " must be a decimal integer")
			}
		}
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			return s, errors.New(f.k + " must be a decimal integer")
		}
		*f.p = n
	}
	for _, f := range []struct {
		k, d string
		p    *time.Duration
	}{{"CHECKOUT_REQUEST_TIMEOUT", "10s", &s.RequestTimeout}, {"STRIPE_CALL_TIMEOUT", "2s", &s.CallTimeout}, {"STRIPE_RETRY_BUDGET", "7s", &s.RetryBudget}} {
		d, e := time.ParseDuration(value(f.k, f.d))
		if e != nil {
			return s, errors.New(f.k + " must be a duration")
		}
		*f.p = d
	}
	attempts := value("STRIPE_MAX_ATTEMPTS", "3")
	for _, r := range attempts {
		if r < '0' || r > '9' {
			return s, errors.New("STRIPE_MAX_ATTEMPTS must be an integer")
		}
	}
	n, e := strconv.Atoi(attempts)
	if e != nil {
		return s, errors.New("STRIPE_MAX_ATTEMPTS must be an integer")
	}
	s.MaxAttempts = n
	return s, nil
}
func (c Config) ValidateServing() error {
	if e := c.ValidateBasic(); e != nil {
		return e
	}
	if c.checkoutErr != nil {
		return c.checkoutErr
	}
	s := c.checkout
	if !strings.HasPrefix(s.SecretKey, "sk_test_") || len(s.SecretKey) == len("sk_test_") {
		return errors.New("STRIPE_SECRET_KEY must be a sandbox secret key")
	}
	for _, b := range s.SecretKey {
		if b < 33 || b > 126 {
			return errors.New("STRIPE_SECRET_KEY must be a sandbox secret key")
		}
	}
	u, e := url.Parse(s.BaseURL)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Path != "" && u.Path != "/" {
		return errors.New("APP_BASE_URL must be a local HTTP origin")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return errors.New("APP_BASE_URL must be a local HTTP origin")
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return errors.New("APP_BASE_URL must have a valid port")
		}
	}
	if s.Currency != "usd" {
		return errors.New("PAYMENT_CURRENCY must be usd")
	}
	if s.MinAmount < 50 || s.MinAmount > 100000 || s.MaxAmount < 50 || s.MaxAmount > 100000 || s.MinAmount > s.MaxAmount {
		return errors.New("PAYMENT_MIN_AMOUNT and PAYMENT_MAX_AMOUNT must be within supported limits")
	}
	if s.RequestTimeout < 2*time.Second || s.RequestTimeout > 10*time.Second {
		return errors.New("CHECKOUT_REQUEST_TIMEOUT is outside its supported range")
	}
	if s.CallTimeout < 100*time.Millisecond || s.CallTimeout > 2*time.Second {
		return errors.New("STRIPE_CALL_TIMEOUT is outside its supported range")
	}
	if s.RetryBudget < 500*time.Millisecond || s.RetryBudget > 7*time.Second || s.RetryBudget > s.RequestTimeout-time.Second || s.CallTimeout > s.RetryBudget {
		return errors.New("STRIPE_RETRY_BUDGET must fit the request and call budgets")
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 3 {
		return errors.New("STRIPE_MAX_ATTEMPTS must be within its supported range")
	}
	if c.ReadTimeout < s.RequestTimeout+time.Second || c.WriteTimeout < s.RequestTimeout+time.Second {
		return errors.New("HTTP_READ_TIMEOUT and HTTP_WRITE_TIMEOUT must allow request finalization")
	}
	return nil
}
