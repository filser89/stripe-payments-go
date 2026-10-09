package web

import (
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOrderReadResponses(t *testing.T) { // HTTP-002 HTTP-006 LIFE-008
	for _, tc := range []struct {
		name, state               string
		resume, retry, newAttempt bool
	}{{"prepared", "prepared", false, true, false}, {"open", "open", true, true, false}, {"unresolved", "unresolved", false, true, false}, {"complete_unpaid", "complete_unpaid", false, false, false}, {"expired", "expired", false, false, true}, {"rejected", "rejected", false, false, true}, {"paid", "paid", false, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			f.View.Operation.State = tc.state
			f.View.CanResume = tc.resume
			f.View.CanRetrySameOperation = tc.retry
			f.View.CanStartNewAttempt = tc.newAttempt
			if tc.state == "paid" {
				f.View.Order.Status = "paid"
				f.View.Operation.CheckoutURL = nil
			}
			if tc.state == "prepared" || tc.state == "unresolved" {
				f.View.Operation.SessionID = nil
				f.View.Operation.CheckoutURL = nil
			}
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request("GET", "/api/orders/"+f.View.Order.ID, "", true))
			require.Equal(t, 200, w.Code)
			v := testutil.JSON(t, w)
			require.Equal(t, tc.state, v["operation"].(map[string]any)["state"])
			require.Equal(t, tc.resume, v["can_resume"])
			require.Equal(t, tc.retry, v["can_retry_same_operation"])
			require.Equal(t, tc.newAttempt, v["can_start_new_attempt"])
			require.EqualValues(t, 1, f.Read.Load())
			require.Zero(t, f.Created.Load())
			require.Zero(t, f.Continued.Load())
			require.Zero(t, f.Histories.Load())
			if f.View.Operation.SessionID == nil {
				require.Nil(t, v["operation"].(map[string]any)["stripe_session_id"])
			}
		})
	}
}
