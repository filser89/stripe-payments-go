package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
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
			predecessor := v["operation"].(map[string]any)["stripe_session_id"].(string)
			j.Stripe.Configure(func(s *testutil.StripeServer) {
				s.Mutation = func(obj map[string]any) {
					if obj["id"] != predecessor {
						return
					}
					obj["status"] = tc.status
					obj["payment_status"] = tc.payment
					if tc.status != "open" {
						obj["url"] = ""
					}
					if tc.payment == "paid" {
						obj["payment_intent"] = "pi_test_paid"
					}
				}
			})
			key := uuid.NewString()
			w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			if tc.allow {
				require.Equal(t, 201, w.Code)
				result := testutil.JSON(t, w)
				require.NotEqual(t, original, result["operation"].(map[string]any)["id"])
				require.Equal(t, "open", result["operation"].(map[string]any)["state"])
				require.NotEmpty(t, result["operation"].(map[string]any)["checkout_url"])
				require.NotEqual(t, predecessor, result["operation"].(map[string]any)["stripe_session_id"])
				require.Equal(t, 2, j.Stripe.LogicalObjects())
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

// ID-002 ID-004 LIFE-003 LIFE-004 LIFE-005 HTTP-004
func TestConnectedGlobalHistoricalAndReusableBindings(t *testing.T) {
	j := journey(t)
	initial := uuid.NewString()
	v, status := create(t, j, initial)
	require.Equal(t, 201, status)
	id, original := envelopeIDs(t, v)
	second, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	other, _ := envelopeIDs(t, second)
	continuation := func(order, key string) *httptest.ResponseRecorder {
		return testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+order+"/checkout", `{"request_key":"`+key+`"}`, true))
	}
	assertConflict := func(w *httptest.ResponseRecorder) {
		require.Equal(t, 409, w.Code)
		require.Equal(t, "idempotency_conflict", testutil.JSON(t, w)["error"].(map[string]any)["code"])
	}
	before := j.DB.Durable(t)
	wires := len(j.Stripe.Wires())
	assertConflict(continuation(id, initial))
	assertConflict(continuation(other, initial))
	require.Equal(t, before, j.DB.Durable(t))
	require.Equal(t, wires, len(j.Stripe.Wires()))
	bound := uuid.NewString()
	w := continuation(id, bound)
	require.Equal(t, 200, w.Code)
	b, err := j.Repo.LoadBinding(context.Background(), bound)
	require.NoError(t, err)
	require.Equal(t, id, b.OrderID)
	require.Equal(t, original, b.OperationID)
	require.Equal(t, "/api/orders/"+id+"/checkout", b.Target)
	before = j.DB.Durable(t)
	wires = len(j.Stripe.Wires())
	assertConflict(continuation(other, bound))
	bad := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(bound, "Single café product", 2500), true))
	assertConflict(bad)
	require.Equal(t, before, j.DB.Durable(t))
	require.Equal(t, wires, len(j.Stripe.Wires()))
	session := v["operation"].(map[string]any)["stripe_session_id"].(string)
	j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
		if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+session {
			return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "complete"; o["payment_status"] = "unpaid"; o["url"] = "" }}
		}
		return testutil.StripeReply{}
	})
	blocked := uuid.NewString()
	w = continuation(id, blocked)
	require.Equal(t, 409, w.Code)
	require.Equal(t, "checkout_blocked", testutil.JSON(t, w)["error"].(map[string]any)["code"])
	_, err = j.Repo.LoadBinding(context.Background(), blocked)
	require.Error(t, err)
	j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
		if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+session {
			return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["payment_status"] = "unpaid"; o["url"] = "" }}
		}
		return testutil.StripeReply{}
	})
	w = continuation(id, blocked)
	require.Equal(t, 201, w.Code)
	fresh := testutil.JSON(t, w)
	_, newOp := envelopeIDs(t, fresh)
	require.NotEqual(t, original, newOp)
	newBinding, err := j.Repo.LoadBinding(context.Background(), blocked)
	require.NoError(t, err)
	require.Equal(t, newOp, newBinding.OperationID)
	wires = len(j.Stripe.Wires())
	w = continuation(id, bound)
	require.Equal(t, 200, w.Code)
	historical := testutil.JSON(t, w)
	_, oldOp := envelopeIDs(t, historical)
	require.Equal(t, original, oldOp)
	require.Nil(t, historical["operation"].(map[string]any)["checkout_url"])
	retained, err := j.Repo.LoadBinding(context.Background(), bound)
	require.NoError(t, err)
	retained.View = payment.View{}
	b.View = payment.View{}
	require.Equal(t, b, retained)
	for _, wire := range j.Stripe.Wires()[wires:] {
		require.Equal(t, "GET", wire.Method, "historical binding cannot create current work")
		require.Equal(t, "/v1/checkout/sessions/"+session, wire.Path)
	}
	current, err := j.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, newOp, current.Operation.ID)
	tighter := j.Options
	tighter.MinAmount = 5000
	tighter.MaxAmount = 6000
	restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, tighter)
	before = j.DB.Durable(t)
	changed := testutil.Response(t, restart.Handler, testutil.Request("POST", "/api/orders", purchaseBody(initial, "Single café product", 2501), true))
	assertConflict(changed)
	require.Equal(t, before, j.DB.Durable(t))
}

// LIFE-007 REC-004
func TestConnectedSDKCorrelationMismatchBlocksReplacement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"wrong_client", func(o map[string]any) { o["client_reference_id"] = "wrong" }},
		{"wrong_order_metadata", func(o map[string]any) {
			o["metadata"] = map[string]string{"order_id": "wrong", "operation_id": "wrong"}
		}},
		{"missing_operation_metadata", func(o map[string]any) { o["metadata"] = map[string]string{} }},
		{"wrong_mode", func(o map[string]any) { o["mode"] = "subscription" }},
		{"wrong_amount", func(o map[string]any) { o["amount_total"] = 2501 }},
		{"wrong_currency", func(o map[string]any) { o["currency"] = "eur" }},
		{"live_evidence", func(o map[string]any) { o["livemode"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("creation", func(t *testing.T) {
				j := journey(t)
				j.Stripe.SetScript(func(testutil.Wire) testutil.StripeReply { return testutil.StripeReply{Mutation: tc.mutate} })
				v, status := create(t, j, uuid.NewString())
				require.Equal(t, 202, status)
				id, op := envelopeIDs(t, v)
				view, err := j.Repo.LoadOrder(context.Background(), id)
				require.NoError(t, err)
				require.Equal(t, "unresolved", view.Operation.State)
				require.Equal(t, op, view.Operation.ID)
				require.True(t, view.NeedsInvestigation)
				require.False(t, view.CanStartNewAttempt)
				require.False(t, view.CanResume)
				require.Nil(t, v["operation"].(map[string]any)["checkout_url"])
				require.Equal(t, 1, j.Stripe.LogicalObjects())
			})
			t.Run("retrieval", func(t *testing.T) {
				j := journey(t)
				v, status := create(t, j, uuid.NewString())
				require.Equal(t, 201, status)
				id, op := envelopeIDs(t, v)
				j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
					if w.Method == "GET" {
						return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["url"] = ""; tc.mutate(o) }}
					}
					return testutil.StripeReply{}
				})
				key := uuid.NewString()
				w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
				require.Equal(t, 409, w.Code)
				view, err := j.Repo.LoadOrder(context.Background(), id)
				require.NoError(t, err)
				require.Equal(t, op, view.Operation.ID)
				require.Equal(t, "unresolved", view.Operation.State)
				require.True(t, view.NeedsInvestigation)
				require.False(t, view.CanResume)
				require.False(t, view.CanStartNewAttempt)
				_, err = j.Repo.LoadBinding(context.Background(), key)
				require.Error(t, err)
				require.Equal(t, 1, j.Stripe.LogicalObjects())
				require.Len(t, j.Stripe.Wires(), 2)
				require.Equal(t, "GET", j.Stripe.Wires()[1].Method)
			})
		})
	}
}

func TestLegacyRejectedCheckoutContinuesWithCardFilter(t *testing.T) {
	j := journey(t)
	intent := testutil.Intent()
	intent.Operation.Snapshot.AllowedPaymentMethodTypes = nil
	_, err := j.Repo.AcceptInitial(context.Background(), intent)
	require.NoError(t, err)
	_, status := create(t, j, intent.Binding.Key)
	require.Equal(t, 502, status)
	before, err := j.Repo.LoadOrder(context.Background(), intent.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "rejected", before.Operation.State)
	response := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders/"+intent.Order.ID+"/checkout", `{"request_key":"`+uuid.NewString()+`"}`, true))
	require.Equal(t, 201, response.Code)
	orderID, operationID := envelopeIDs(t, testutil.JSON(t, response))
	require.Equal(t, intent.Order.ID, orderID)
	require.NotEqual(t, intent.Operation.ID, operationID)
	after, err := j.Repo.LoadOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Equal(t, []string{"card"}, after.Operation.Snapshot.AllowedPaymentMethodTypes)
	require.Equal(t, "open", after.Operation.State)
	original, err := j.Repo.LoadBinding(context.Background(), intent.Binding.Key)
	require.NoError(t, err)
	require.Equal(t, before.Operation, original.View.Operation)
	wires := j.Stripe.Wires()
	require.Len(t, wires, 2)
	require.Equal(t, "card", wires[0].Form.Get("payment_method_types[0]"))
	testutil.CheckWire(t, wires[1], after.Operation.Snapshot)
	require.NotEqual(t, wires[0].Header.Get("Idempotency-Key"), wires[1].Header.Get("Idempotency-Key"))
	require.Equal(t, 1, j.Stripe.LogicalObjects())
}
