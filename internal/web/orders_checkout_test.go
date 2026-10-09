package web

import (
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCheckoutContinuationResponses(t *testing.T) { // HTTP-002 HTTP-003 HTTP-004 ID-004 LIFE-005
	for _, tc := range []struct {
		name, state, code string
		status            int
	}{{"bound_open", "open", "", 200}, {"bound_unresolved", "unresolved", "", 202}, {"prepared_after_refresh_budget", "prepared", "", 202}, {"blocked_before_acceptance", "open", "checkout_blocked", 409}, {"bound_rejection_replay", "rejected", "checkout_rejected", 502}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			f.Outcome.NewlyAccepted = false
			f.Outcome.Operation.State = tc.state
			if tc.status == 202 {
				f.Outcome.Pending = true
				f.Outcome.Established = false
			}
			if tc.code != "" {
				f.Err = checkoutError(tc.code, f)
			}
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request("POST", "/api/orders/"+f.Outcome.Order.ID+"/checkout", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, true))
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, f.Created.Load())
			require.EqualValues(t, 1, f.Continued.Load())
			v := testutil.JSON(t, w)
			if tc.status < 400 {
				require.Equal(t, f.Outcome.Operation.ID, v["operation"].(map[string]any)["id"])
				require.Equal(t, f.Outcome.Order.ID, v["order"].(map[string]any)["id"])
				if tc.status == 202 {
					require.Equal(t, "1", w.Header().Get("Retry-After"))
				}
			} else {
				require.Equal(t, tc.code, v["error"].(map[string]any)["code"])
			}
		})
	}
}
