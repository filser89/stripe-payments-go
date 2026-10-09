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
			require.Equal(t, f.Page.OrderID, v["order_id"])
			require.Equal(t, float64(next), v["next_after"])
			entries := v["entries"].([]any)
			if tc.empty {
				require.Empty(t, entries)
			} else {
				require.Len(t, entries, 1)
				e := entries[0].(map[string]any)
				require.ElementsMatch(t, []string{"sequence", "kind", "recorded_at", "order_id", "operation_id", "from_state", "to_state", "stripe_session_id", "stripe_payment_intent_id", "stripe_event_id", "stripe_request_id", "failure_code"}, checkoutKeys(e))
				assertHistoryEntry(t, e, f.Page.Entries[0])
				require.Equal(t, "prepared", e["from_state"])
				require.Equal(t, "open", e["to_state"])
				require.Nil(t, e["stripe_event_id"])
				require.Nil(t, e["failure_code"])
			}
		})
	}
}

func assertHistoryEntry(t *testing.T, got map[string]any, want payment.HistoryEntry) {
	t.Helper()
	require.Equal(t, float64(want.Sequence), got["sequence"])
	require.Equal(t, want.Kind, got["kind"])
	require.Equal(t, want.OrderID, got["order_id"])
	stamp, ok := got["recorded_at"].(string)
	require.True(t, ok)
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	require.NoError(t, err)
	require.Equal(t, time.UTC, parsed.Location())
	require.True(t, want.ObservedAt.Equal(parsed))
	if want.OperationID == "" {
		require.Nil(t, got["operation_id"])
	} else {
		require.Equal(t, want.OperationID, got["operation_id"])
	}
	for key, value := range map[string]*string{"from_state": want.FromState, "to_state": want.ToState, "stripe_session_id": want.SessionID, "stripe_payment_intent_id": want.PaymentIntentID, "stripe_event_id": want.EventID, "stripe_request_id": want.RequestID, "failure_code": want.FailureCode} {
		if value == nil {
			require.Nil(t, got[key], key)
		} else {
			require.Equal(t, *value, got[key], key)
		}
	}
}
func TestOrderHistoryKindsAndIdentifiers(t *testing.T) { // HTTP-006 DATA-004
	for _, tc := range []struct {
		name, kind string
		populated  bool
	}{
		{"order_created", "order_created", false}, {"prepared", "operation_prepared", true}, {"dispatch", "dispatch_started", true}, {"transition", "operation_state_changed", true}, {"paid", "payment_confirmed", true}, {"recovery", "recovery_recorded", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			entry := payment.HistoryEntry{Sequence: 42, OrderID: f.View.Order.ID, Kind: tc.kind, ObservedAt: time.Date(2026, 10, 9, 12, 34, 56, 123456789, time.FixedZone("fixture", 3600))}
			if tc.populated {
				entry.OperationID = f.View.Operation.ID
				entry.FromState = testutil.Pointer("unresolved")
				entry.ToState = testutil.Pointer("open")
				entry.SessionID = testutil.Pointer("cs_test_observed")
				entry.PaymentIntentID = testutil.Pointer("pi_test_observed")
				entry.EventID = testutil.Pointer("evt_test_observed")
				entry.RequestID = testutil.Pointer("req_test_observed")
				entry.FailureCode = testutil.Pointer("temporarily_unavailable")
			}
			f.Page.Entries = []payment.HistoryEntry{entry}
			f.Page.NextAfter = testutil.Pointer(int64(42))
			h, _ := checkoutHandler(t, f, 2*time.Second)
			w := testutil.Response(t, h, testutil.Request("GET", "/api/orders/"+entry.OrderID+"/history", "", true))
			require.Equal(t, 200, w.Code)
			v := testutil.JSON(t, w)
			require.Equal(t, entry.OrderID, v["order_id"])
			entries := v["entries"].([]any)
			require.Len(t, entries, 1)
			assertHistoryEntry(t, entries[0].(map[string]any), entry)
		})
	}
}
