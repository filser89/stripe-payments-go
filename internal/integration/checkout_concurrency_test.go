package integration_test

import (
	"context"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectedCheckoutConcurrency(t *testing.T) { // HTTP-003 ID-001 ID-002 LIFE-004 LIFE-006 DATA-002 DATA-003 DATA-005 FND-004
	t.Run("independent_servers_identical_key_while_wire_outstanding", func(t *testing.T) {
		j := journey(t)
		other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		barrier := testutil.NewBarrier()
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		})
		defer barrier.Release()
		key := uuid.NewString()
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			w := httptest.NewRecorder()
			j.Handler.ServeHTTP(w, testutil.Request("POST", "/api/orders", purchaseBody(key, "Single café product", 2500), true))
			done <- w
		}()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("first real wire did not arrive")
		}
		testutil.NoIdleTransaction(t, j.DB.Independent(t))
		binding, err := other.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		require.NotEmpty(t, binding.OperationID)
		counts := j.DB.Counts(t)
		require.Equal(t, 1, counts["payment_orders"])
		require.Equal(t, 1, counts["payment_operations"])
		v, status := create(t, other, key)
		require.Equal(t, 202, status)
		id, op := envelopeIDs(t, v)
		require.Equal(t, binding.OrderID, id)
		require.Equal(t, binding.OperationID, op)
		require.Len(t, j.Stripe.Wires(), 1)
		conflict := testutil.Response(t, other.Handler, testutil.Request("POST", "/api/orders", purchaseBody(key, "changed", 2500), true))
		require.Equal(t, 409, conflict.Code)
		require.Equal(t, counts, j.DB.Counts(t))
		barrier.Release()
		select {
		case res := <-done:
			require.Equal(t, 201, res.Code)
			id2, op2 := envelopeIDs(t, testutil.JSON(t, res))
			require.Equal(t, id, id2)
			require.Equal(t, op, op2)
		case <-time.After(3 * time.Second):
			t.Fatal("first checkout work did not join")
		}
		require.Equal(t, 1, j.Stripe.LogicalObjects())
	})
	t.Run("independent_success_while_other_network_blocked", func(t *testing.T) {
		j := journey(t)
		other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		first := testutil.Intent()
		_, err := j.Repo.AcceptInitial(context.Background(), first)
		require.NoError(t, err)
		blockedKey := first.Operation.StripeKey
		barrier := testutil.NewBarrier()
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.Before = func(ctx context.Context, w testutil.Wire) {
				if w.Header.Get("Idempotency-Key") == blockedKey {
					_ = barrier.Wait(ctx)
				}
			}
		})
		done := make(chan error, 1)
		go func() { _, e := j.Service.Continue(context.Background(), first.Order.ID, uuid.NewString()); done <- e }()
		defer barrier.Release()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("first operation not held")
		}
		v, status := create(t, other, uuid.NewString())
		require.Equal(t, 201, status)
		id, _ := envelopeIDs(t, v)
		require.NotEqual(t, first.Order.ID, id)
		testutil.NoIdleTransaction(t, j.DB.Independent(t))
		barrier.Release()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("blocked operation did not join")
		}
	})
}

// DATA-005 LIFE-003 LIFE-006
func TestConnectedBlockedRetrievalAllowsIndependentProgress(t *testing.T) {
	j := journey(t)
	other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	id, op := envelopeIDs(t, v)
	barrier := testutil.NewBarrier()
	defer barrier.Release()
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, w testutil.Wire) {
			if w.Method == "GET" {
				_ = barrier.Wait(ctx)
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := j.Service.Continue(ctx, id, uuid.NewString()); done <- e }()
	select {
	case <-barrier.Arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("real retrieval not held")
	}
	testutil.NoIdleTransaction(t, j.DB.Independent(t))
	before, err := other.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	independent, status := create(t, other, uuid.NewString())
	require.Equal(t, 201, status)
	otherID, _ := envelopeIDs(t, independent)
	require.NotEqual(t, id, otherID)
	testutil.NoIdleTransaction(t, j.DB.Independent(t))
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retrieval work did not join")
	}
	select {
	case <-barrier.Done:
	case <-time.After(time.Second):
		t.Fatal("retrieval peer did not join")
	}
	after, err := j.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, op, after.Operation.ID)
	require.Equal(t, before.Operation.SessionID, after.Operation.SessionID)
	require.Equal(t, "unpaid", after.Order.Status)
	saved, err := j.Repo.LoadOrder(context.Background(), otherID)
	require.NoError(t, err)
	require.Equal(t, "open", saved.Operation.State)
}

// DATA-003 LIFE-006 FND-004
func TestConnectedDelayedWireCannotOverwritePaidOrOwner(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"paid_success", `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`},
		{"owner_success", `UPDATE payment_operations SET owner_token='replacement-owner',version=version+1 WHERE order_id=$1`},
		{"paid_failure", `UPDATE payment_orders SET payment_status='paid' WHERE id=$1`},
		{"owner_failure", `UPDATE payment_operations SET owner_token='replacement-owner',version=version+1 WHERE order_id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			barrier := testutil.NewBarrier()
			defer barrier.Release()
			j.Stripe.Configure(func(s *testutil.StripeServer) {
				s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
			})
			if strings.Contains(tc.name, "failure") {
				j.Stripe.SetScript(func(testutil.Wire) testutil.StripeReply {
					return testutil.StripeReply{Status: 400, Body: `{"error":{"type":"invalid_request_error","message":"sensitive-sentinel"}}`}
				})
			}
			key := uuid.NewString()
			done := make(chan payment.Outcome, 1)
			go func() { o, _ := j.Service.Create(context.Background(), createInput(key)); done <- o }()
			select {
			case <-barrier.Arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("external result not delayed")
			}
			binding, err := j.Repo.LoadBinding(context.Background(), key)
			require.NoError(t, err)
			_, err = j.DB.Independent(t).Exec(context.Background(), tc.sql, binding.OrderID)
			require.NoError(t, err)
			before := j.DB.Durable(t)
			barrier.Release()
			select {
			case out := <-done:
				require.False(t, out.CanResume)
				require.False(t, out.CanStartNewAttempt)
				require.Nil(t, out.Operation.CheckoutURL)
			case <-time.After(3 * time.Second):
				t.Fatal("stale result did not join")
			}
			require.Equal(t, before, j.DB.Durable(t), "stale result itself cannot change current owner, business or history")
			requireMeaningfulLog(t, j.Logs, "", binding.OrderID, binding.OperationID, "ownership", "owner", "stale", "paid")
			require.Equal(t, 1, j.DB.Counts(t)["payment_operations"])
		})
	}
}

// LIFE-004 DATA-002 DATA-003
func TestConnectedCompetingContinuationKeysAllocateOneReplacement(t *testing.T) {
	j := journey(t)
	other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	id, old := envelopeIDs(t, v)
	predecessor := v["operation"].(map[string]any)["stripe_session_id"].(string)
	barrier := testutil.NewBarrier()
	defer barrier.Release()
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, w testutil.Wire) {
			if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+predecessor {
				_ = barrier.Wait(ctx)
			}
		}
	})
	j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
		if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+predecessor {
			return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["payment_status"] = "unpaid"; o["url"] = "" }}
		}
		return testutil.StripeReply{}
	})
	keys := []string{uuid.NewString(), uuid.NewString()}
	type result struct {
		w   *httptest.ResponseRecorder
		key string
	}
	done := make(chan result, 2)
	before := len(j.Stripe.Wires())
	launch := func(app *checkoutJourney, key string) {
		go func() {
			w := httptest.NewRecorder()
			app.Handler.ServeHTTP(w, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			done <- result{w, key}
		}()
	}
	launch(j, keys[0])
	select {
	case <-barrier.Arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("first eligible retrieval not held")
	}
	launch(other, keys[1])
	require.Eventually(t, func() bool { return len(done) > 0 || len(j.Stripe.Wires()) >= before+2 }, 2*time.Second, 10*time.Millisecond, "second app overlaps the held continuation through a DB owner result or real retrieval")
	testutil.NoIdleTransaction(t, j.DB.Independent(t))
	barrier.Release()
	created := 0
	results := []result{}
	for range 2 {
		select {
		case r := <-done:
			results = append(results, r)
			if r.w.Code == 201 {
				created++
			}
			require.Contains(t, []int{200, 201, 202, 409}, r.w.Code)
		case <-time.After(4 * time.Second):
			t.Fatal("continuations did not join")
		}
	}
	require.Equal(t, 1, created)
	current, err := j.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	require.NotEqual(t, old, current.Operation.ID)
	require.Equal(t, "open", current.Operation.State)
	require.NotNil(t, current.Operation.CheckoutURL)
	require.Equal(t, 2, j.Stripe.LogicalObjects())
	require.Equal(t, 2, j.DB.Counts(t)["payment_operations"])
	for _, r := range results {
		b, e := j.Repo.LoadBinding(context.Background(), r.key)
		if r.w.Code == 409 {
			require.Error(t, e)
		} else {
			require.NoError(t, e)
			_, responseOp := envelopeIDs(t, testutil.JSON(t, r.w))
			require.Equal(t, b.OperationID, responseOp)
			if r.w.Code == 201 {
				require.Equal(t, current.Operation.ID, responseOp)
			} else {
				require.Contains(t, []string{old, current.Operation.ID}, responseOp, "in-flight or historical acceptance preserves its actual binding")
				if responseOp == old && r.w.Code == 200 {
					body := testutil.JSON(t, r.w)
					require.Nil(t, body["operation"].(map[string]any)["checkout_url"])
					require.False(t, body["can_resume"].(bool))
				}
			}
		}
	}
	history, err := j.Repo.ReadHistory(context.Background(), id, 0, 100)
	require.NoError(t, err)
	prepared := 0
	for _, h := range history.Entries {
		if h.OperationID == current.Operation.ID && h.Kind == "operation_prepared" {
			prepared++
		}
	}
	require.Equal(t, 1, prepared)
}

// LIFE-004 LIFE-006 DATA-003
func TestDelayedEligibilityCannotOverwriteNewCurrentOperation(t *testing.T) {
	j := journey(t)
	other := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	id, old := envelopeIDs(t, v)
	session := v["operation"].(map[string]any)["stripe_session_id"].(string)
	barrier := testutil.NewBarrier()
	defer barrier.Release()
	var gets atomic.Int32
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, w testutil.Wire) {
			if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+session && gets.Add(1) == 1 {
				_ = barrier.Wait(ctx)
			}
		}
	})
	j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
		if w.Method == "GET" && w.Path == "/v1/checkout/sessions/"+session {
			return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["payment_status"] = "unpaid"; o["url"] = "" }}
		}
		return testutil.StripeReply{}
	})
	firstKey := uuid.NewString()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		j.Handler.ServeHTTP(w, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+firstKey+`"}`, true))
		done <- w
	}()
	select {
	case <-barrier.Arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("stale eligibility retrieval not held")
	}
	// A coordination takeover models the first caller losing its persisted claim;
	// subsequent current-operation allocation still uses normal guarded repository paths.
	_, err := j.DB.Independent(t).Exec(context.Background(), `UPDATE payment_operations SET owner_token='',version=version+1 WHERE id=$1`, old)
	require.NoError(t, err)
	replacementKey := uuid.NewString()
	w := testutil.Response(t, other.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+replacementKey+`"}`, true))
	require.Equal(t, 201, w.Code)
	_, replacement := envelopeIDs(t, testutil.JSON(t, w))
	require.NotEqual(t, old, replacement)
	before := j.DB.Durable(t)
	barrier.Release()
	select {
	case stale := <-done:
		require.Contains(t, []int{200, 202, 409, 503}, stale.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("stale eligibility result not joined")
	}
	afterRows := j.DB.Durable(t)
	require.Equal(t, before["payment_history"], afterRows["payment_history"], "stale eligibility cannot add a replacement transition")
	require.Equal(t, before["payment_orders"], afterRows["payment_orders"])
	require.Equal(t, 2, len(afterRows["payment_operations"]), "stale eligibility cannot allocate another operation")
	current, err := j.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, replacement, current.Operation.ID)
	require.Equal(t, "open", current.Operation.State)
	require.NotNil(t, current.Operation.CheckoutURL)
	require.Equal(t, 2, j.Stripe.LogicalObjects())
	if binding, e := j.Repo.LoadBinding(context.Background(), firstKey); e == nil {
		require.Contains(t, []string{old, replacement}, binding.OperationID)
		if binding.OperationID == replacement {
			refreshed := false
			for _, wire := range j.Stripe.Wires() {
				if wire.Method == "GET" && wire.Path == "/v1/checkout/sessions/"+*current.Operation.SessionID {
					refreshed = true
				}
			}
			require.True(t, refreshed, "binding to a new current operation needs its own validated retrieval")
		}
	}
}
