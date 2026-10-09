package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConnectedCheckoutLifecycle(t *testing.T) { // LIFE-001 LIFE-002 LIFE-003 LIFE-004 LIFE-005 LIFE-006 LIFE-007 LIFE-008 LIFE-009 ID-004 STR-002 DATA-001 DATA-003
	for _, tc := range []struct {
		name, status, payment string
		allow                 bool
	}{{"open_refresh", "open", "unpaid", false}, {"verified_expired", "expired", "unpaid", true}, {"complete_unpaid_blocks", "complete", "unpaid", false}, {"paid_evidence_without_local_confirmation", "complete", "paid", false}, {"unknown_combination", "open", "paid", false}} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			initialKey := uuid.NewString()
			v, status := create(t, j, initialKey)
			require.Equal(t, 201, status)
			id, original := envelopeIDs(t, v)
			j.Stripe.Mutation = func(obj map[string]any) {
				obj["status"] = tc.status
				obj["payment_status"] = tc.payment
				if tc.status != "open" {
					obj["url"] = ""
				}
				if tc.payment == "paid" {
					obj["payment_intent"] = "pi_test_paid"
				}
			}
			key := uuid.NewString()
			w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			if tc.allow {
				require.Equal(t, 201, w.Code)
				result := testutil.JSON(t, w)
				require.NotEqual(t, original, result["operation"].(map[string]any)["id"])
				old, status := create(t, j, initialKey)
				require.Equal(t, 200, status)
				require.Equal(t, original, old["operation"].(map[string]any)["id"])
				require.False(t, old["can_resume"].(bool))
			} else if tc.status == "open" && tc.payment == "unpaid" {
				require.Equal(t, 200, w.Code)
				result := testutil.JSON(t, w)
				require.Equal(t, original, result["operation"].(map[string]any)["id"])
				require.Equal(t, 1, j.Stripe.LogicalObjects())
			} else {
				require.Equal(t, 409, w.Code)
				require.Equal(t, 1, j.Stripe.LogicalObjects())
				_, err := j.Repo.LoadBinding(context.Background(), key)
				require.Error(t, err, "blocked fresh key must stay unbound")
			}
			view, err := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, "unpaid", view.Order.Status, "checkout evidence cannot confirm payment in Feature2")
			require.Equal(t, "GET", j.Stripe.Wires()[1].Method)
		})
	}
	t.Run("paid_prerequisite_blocks_and_hides_url", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		v, status := create(t, j, key)
		require.Equal(t, 201, status)
		id, _ := envelopeIDs(t, v)
		_, err := j.DB.Independent(t).Exec(context.Background(), `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`, id)
		require.NoError(t, err)
		wireBefore := len(j.Stripe.Wires())
		w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+uuid.NewString()+`"}`, true))
		require.Equal(t, 409, w.Code)
		replay, status := create(t, j, key)
		require.Equal(t, 200, status)
		require.Nil(t, replay["operation"].(map[string]any)["checkout_url"])
		for _, flag := range []string{"can_resume", "can_retry_same_operation", "can_start_new_attempt"} {
			require.False(t, replay[flag].(bool))
		}
		require.Equal(t, wireBefore, len(j.Stripe.Wires()))
	})
}
