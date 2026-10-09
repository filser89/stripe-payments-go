package web

import (
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCheckoutCreateResponses(t *testing.T) { // HTTP-001 HTTP-002 HTTP-003
	for _, tc := range []struct {
		name                     string
		newly, pending, rejected bool
		state                    string
		status                   int
	}{{"new_open", true, false, false, "open", 201}, {"established_replay", false, false, false, "open", 200}, {"prepared", true, true, false, "prepared", 202}, {"unresolved", true, true, false, "unresolved", 202}, {"in_flight", false, true, false, "unresolved", 202}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			f.Outcome.NewlyAccepted = tc.newly
			f.Outcome.Established = !tc.pending
			f.Outcome.Pending = tc.pending
			f.Outcome.Operation.State = tc.state
			if tc.pending {
				f.Outcome.Operation.SessionID = nil
				f.Outcome.Operation.CheckoutURL = nil
				f.Outcome.Operation.ExpiresAt = nil
			}
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request("POST", "/api/orders", `{"description":"Single café product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true))
			require.Equal(t, tc.status, w.Code)
			require.EqualValues(t, 1, f.Created.Load())
			require.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotEmpty(t, w.Header().Get("X-Request-ID"))
			v := testutil.JSON(t, w)
			require.ElementsMatch(t, []string{"order", "operation", "can_resume", "can_retry_same_operation", "can_start_new_attempt"}, checkoutKeys(v))
			o := v["order"].(map[string]any)
			require.ElementsMatch(t, []string{"id", "description", "amount", "currency", "payment_status", "created_at", "updated_at"}, checkoutKeys(o))
			require.Equal(t, f.Outcome.Order.ID, o["id"])
			require.Equal(t, "usd", o["currency"])
			require.Equal(t, float64(2500), o["amount"])
			require.Equal(t, "unpaid", o["payment_status"])
			op := v["operation"].(map[string]any)
			require.ElementsMatch(t, []string{"id", "state", "stripe_session_id", "stripe_payment_intent_id", "checkout_url", "first_dispatch_at", "expires_at", "created_at", "updated_at", "failure_code", "investigation_required"}, checkoutKeys(op))
			require.Equal(t, f.Outcome.Operation.ID, op["id"])
			require.Equal(t, tc.state, op["state"])
			require.Nil(t, op["stripe_payment_intent_id"])
			require.Nil(t, op["first_dispatch_at"])
			require.Nil(t, op["failure_code"])
			for _, value := range []any{o["created_at"], o["updated_at"], op["created_at"], op["updated_at"]} {
				stamp, err := time.Parse(time.RFC3339Nano, value.(string))
				require.NoError(t, err)
				require.Equal(t, time.UTC, stamp.Location())
			}
			if tc.pending {
				require.Nil(t, op["stripe_session_id"])
				require.Nil(t, op["checkout_url"])
				require.Equal(t, "/api/orders/"+f.Outcome.Order.ID, w.Header().Get("Location"))
				require.Equal(t, "1", w.Header().Get("Retry-After"))
			} else {
				require.Equal(t, *f.Outcome.Operation.CheckoutURL, op["checkout_url"])
			}
			require.Equal(t, f.Outcome.CanResume, v["can_resume"])
			require.Equal(t, f.Outcome.CanRetrySameOperation, v["can_retry_same_operation"])
			require.Equal(t, f.Outcome.CanStartNewAttempt, v["can_start_new_attempt"])
		})
	}
}
func checkoutKeys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
func TestHTTP004CreateResponse(t *testing.T) { // HTTP-004 SEC-002
	for _, tc := range []struct {
		name, code string
		status     int
		known      bool
	}{{"conflict", "idempotency_conflict", 409, false}, {"blocked", "checkout_blocked", 409, false}, {"rejected", "checkout_rejected", 502, true}, {"acceptance_unavailable", "temporarily_unavailable", 503, false}, {"read_unavailable_known", "temporarily_unavailable", 503, true}, {"missing", "not_found", 404, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			f.Err = checkoutError(tc.code, f)
			if !tc.known {
				f.Outcome.Order.ID = ""
				f.Outcome.Operation.ID = ""
				f.Err = checkoutError(tc.code, f)
			}
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request("POST", "/api/orders", `{"description":"Single café product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true))
			require.Equal(t, tc.status, w.Code)
			v := testutil.JSON(t, w)
			e := v["error"].(map[string]any)
			require.Equal(t, tc.code, e["code"])
			require.NotEmpty(t, e["message"])
			require.NotContains(t, w.Body.String(), "sensitive-sentinel")
			require.NotContains(t, w.Body.String(), "Single café product")
			if tc.known {
				require.Equal(t, f.Outcome.Order.ID, e["order_id"])
				require.Equal(t, f.Outcome.Operation.ID, e["operation_id"])
			} else {
				require.NotContains(t, e, "order_id")
				require.NotContains(t, e, "operation_id")
			}
		})
	}
}
