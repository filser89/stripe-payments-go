package web

import (
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCheckoutEndpointRouting(t *testing.T) { // HTTP-005 SEC-001
	for _, tc := range []struct {
		name, method, path, allow string
		status                    int
	}{{"initial_wrong", "GET", "/api/orders", "POST", 405}, {"checkout_wrong", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/checkout", "POST", 405}, {"order_wrong", "POST", "/api/orders/11111111-1111-4111-8111-111111111111", "GET, HEAD", 405}, {"history_wrong", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/history", "GET, HEAD", 405}, {"unknown", "GET", "/api/unknown", "", 404}, {"malformed_id", "GET", "/api/orders/bad", "", 400}, {"order_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111", "", 200}, {"history_head", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history", "", 200}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request(tc.method, tc.path, "", true))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.allow, w.Header().Get("Allow"))
			if tc.method == "HEAD" {
				require.Empty(t, w.Body.String())
				get := testutil.Response(t, h, testutil.Request("GET", tc.path, "", true))
				require.Equal(t, get.Code, w.Code)
				require.Equal(t, get.Header().Get("Content-Type"), w.Header().Get("Content-Type"))
				require.Equal(t, get.Header().Get("Cache-Control"), w.Header().Get("Cache-Control"))
			}
			if tc.status >= 400 {
				require.Zero(t, f.Effects())
				code := "method_not_allowed"
				if tc.status == 404 {
					code = "not_found"
				}
				if tc.status == 400 {
					code = "invalid_request"
				}
				checkoutLocalError(t, w, code)
			}
		})
	}
}

func TestCheckoutReadErrorHEADParity(t *testing.T) { // HTTP-004 HTTP-005
	for _, tc := range []struct {
		name, path, code string
		status           int
	}{
		{"malformed_id", "/api/orders/bad", "invalid_request", 400},
		{"bad_query", "/api/orders/11111111-1111-4111-8111-111111111111?unknown=1", "invalid_request", 400},
		{"missing_order", "/api/orders/11111111-1111-4111-8111-111111111111", "not_found", 404},
		{"unavailable_order", "/api/orders/11111111-1111-4111-8111-111111111111", "temporarily_unavailable", 503},
		{"missing_history", "/api/orders/11111111-1111-4111-8111-111111111111/history", "not_found", 404},
		{"unavailable_history", "/api/orders/11111111-1111-4111-8111-111111111111/history", "temporarily_unavailable", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			if tc.code != "invalid_request" {
				f.Err = checkoutError(tc.code, f)
			}
			h, _ := checkoutHandler(t, f, 2*time.Second)
			get := testutil.Response(t, h, testutil.Request("GET", tc.path, "", true))
			head := testutil.Response(t, h, testutil.Request("HEAD", tc.path, "", true))
			require.Equal(t, tc.status, get.Code)
			require.Equal(t, get.Code, head.Code)
			checkoutLocalError(t, get, tc.code)
			checkoutHeaders(t, head)
			require.Empty(t, head.Body.String())
			for _, header := range []string{"Content-Type", "Cache-Control", "Allow", "Location", "Retry-After"} {
				require.Equal(t, get.Header().Get(header), head.Header().Get(header))
			}
		})
	}
}
