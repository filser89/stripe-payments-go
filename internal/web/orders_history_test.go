package web

import (
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOrderHistoryResponses(t *testing.T) { // HTTP-006 DATA-004
	for _, tc := range []struct {
		name, query string
		after       int64
		limit       int
		empty       bool
	}{{"default", "", 0, 50, false}, {"first", "?after=0&limit=1", 0, 1, false}, {"next", "?after=8&limit=100", 8, 100, false}, {"empty", "?after=9&limit=50", 9, 50, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			next := tc.after
			if !tc.empty {
				next++
				f.Page.Entries = []payment.HistoryEntry{{Sequence: next, OrderID: f.View.Order.ID, OperationID: f.View.Operation.ID, Kind: "operation_state_changed", ObservedAt: f.View.Order.CreatedAt, FromState: testutil.Pointer("prepared"), ToState: testutil.Pointer("open")}}
			}
			f.Page.NextAfter = &next
			h, _ := checkoutHandler(t, f, time.Second)
			w := testutil.Response(t, h, testutil.Request("GET", "/api/orders/"+f.View.Order.ID+"/history"+tc.query, "", true))
			require.Equal(t, 200, w.Code)
			require.Equal(t, tc.after, f.After)
			require.Equal(t, tc.limit, f.Limit)
			require.EqualValues(t, 1, f.Histories.Load())
			require.Zero(t, f.Created.Load())
			require.Zero(t, f.Continued.Load())
			v := testutil.JSON(t, w)
			require.ElementsMatch(t, []string{"order_id", "entries", "next_after"}, checkoutKeys(v))
			require.Equal(t, float64(next), v["next_after"])
			entries := v["entries"].([]any)
			if tc.empty {
				require.Empty(t, entries)
			} else {
				require.Len(t, entries, 1)
				e := entries[0].(map[string]any)
				require.ElementsMatch(t, []string{"sequence", "kind", "recorded_at", "order_id", "operation_id", "from_state", "to_state", "stripe_session_id", "stripe_payment_intent_id", "stripe_event_id", "stripe_request_id", "failure_code"}, checkoutKeys(e))
				require.Equal(t, "prepared", e["from_state"])
				require.Equal(t, "open", e["to_state"])
				require.Nil(t, e["stripe_event_id"])
				require.Nil(t, e["failure_code"])
			}
		})
	}
}
