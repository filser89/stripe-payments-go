package integration_test

import (
	"context"
	"encoding/json"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/stripeapi"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectedCheckoutRecovery(t *testing.T) { // HTTP-007 ID-001 STR-001 STR-002 STR-003 STR-007 STR-008 REC-001 REC-002 REC-003 REC-004 DATA-001 DATA-004 SEC-002 SEC-003
	t.Run("response_lost_restart_reuses_snapshot", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		j.Stripe.Configure(func(s *testutil.StripeServer) { s.Drop = true })
		v, status := create(t, j, key)
		require.Equal(t, 202, status)
		id, op := envelopeIDs(t, v)
		before, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Nil(t, before.Operation.SessionID)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		require.NotNil(t, before.Operation.FirstDispatchAt)
		require.NotNil(t, before.Operation.LastDispatchAt)
		require.Equal(t, before.Operation.FirstDispatchAt.Unix()+int64((23*time.Hour+59*time.Minute)/time.Second), before.Operation.Snapshot.ExpiresAt)
		j.Stripe.Configure(func(s *testutil.StripeServer) { s.Drop = false })
		options := j.Options
		options.Origin = "http://127.0.0.1:9090"
		options.MinAmount = 5000
		options.MaxAmount = 6000
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, options)
		v, status = create(t, restart, key)
		require.Equal(t, 200, status)
		id2, op2 := envelopeIDs(t, v)
		require.Equal(t, id, id2)
		require.Equal(t, op, op2)
		after, err := restart.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, before.Operation.StripeKey, after.Operation.StripeKey)
		require.Equal(t, testutil.ImmutableSnapshot(before.Operation.Snapshot), testutil.ImmutableSnapshot(after.Operation.Snapshot))
		require.NotNil(t, after.Operation.LastDispatchAt)
		require.True(t, after.Operation.LastDispatchAt.After(*before.Operation.LastDispatchAt), "recovery updates durable last possible dispatch")
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		for _, wire := range j.Stripe.Wires() {
			if wire.Method == "POST" {
				testutil.CheckWire(t, wire, before.Operation.Snapshot)
			}
		}
	})
	t.Run("acceptance_failure_sends_nothing", func(t *testing.T) {
		j := journey(t)
		release := testutil.FailHistory(t, j.DB.Independent(t), true)
		v, status := create(t, j, uuid.NewString())
		require.Equal(t, 503, status)
		require.Equal(t, "temporarily_unavailable", v["error"].(map[string]any)["code"])
		require.Empty(t, j.Stripe.Wires())
		require.Equal(t, map[string]int{"payment_orders": 0, "payment_operations": 0, "payment_request_bindings": 0, "payment_history": 0}, j.DB.Counts(t))
		release()
	})
	t.Run("success_before_result_commit_failure_then_restart", func(t *testing.T) {
		j := journey(t)
		control := j.DB.Independent(t)
		barrier := testutil.NewBarrier()
		defer barrier.Release()
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		})
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
			t.Fatal("dispatch not observed")
		}
		binding, err := j.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		before, err := j.Repo.LoadOrder(context.Background(), binding.OrderID)
		require.NoError(t, err)
		require.NotNil(t, before.Operation.FirstDispatchAt)
		require.NotNil(t, before.Operation.LastDispatchAt)
		testutil.NoIdleTransaction(t, control)
		durable := j.DB.Durable(t)
		release := testutil.FailHistory(t, control, true)
		barrier.Release()
		var w *httptest.ResponseRecorder
		select {
		case w = <-done:
		case <-time.After(4 * time.Second):
			t.Fatal("result work did not join")
		}
		v, status := testutil.JSON(t, w), w.Code
		require.Equal(t, 202, status, "uncommitted SDK result cannot be reported as 201")
		id, op := envelopeIDs(t, v)
		require.Equal(t, binding.OrderID, id)
		require.Equal(t, binding.OperationID, op)
		requireMeaningfulLog(t, j.Logs, w.Header().Get("X-Request-ID"), id, op, w.Code)
		view, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, businessRows(t, durable), businessRows(t, j.DB.Durable(t)), "result failure preserves business state/history; safe ownership release is separate")
		require.Nil(t, view.Operation.SessionID)
		require.Nil(t, view.Operation.CheckoutURL)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		release()
		j.Stripe.Configure(func(s *testutil.StripeServer) { s.Before = nil })
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
		v, status = create(t, restart, key)
		require.Equal(t, 200, status)
		id2, op2 := envelopeIDs(t, v)
		require.Equal(t, id, id2)
		require.Equal(t, op, op2)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		saved, err := restart.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, saved.Operation.SessionID)
		require.Equal(t, testutil.ImmutableSnapshot(view.Operation.Snapshot), testutil.ImmutableSnapshot(saved.Operation.Snapshot))
	})
	t.Run("old_creation_refuses_post_at_safe_boundary", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		j.Stripe.Configure(func(s *testutil.StripeServer) { s.Drop = true })
		v, status := create(t, j, key)
		require.Equal(t, 202, status)
		id, _ := envelopeIDs(t, v)
		saved, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		options := j.Options
		options.Now = func() time.Time { return saved.Operation.FirstDispatchAt.Add(23 * time.Hour) }
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, options)
		wireBefore := len(j.Stripe.Wires())
		v, status = create(t, restart, key)
		require.Equal(t, 202, status)
		require.Equal(t, wireBefore, len(j.Stripe.Wires()))
		require.True(t, v["operation"].(map[string]any)["investigation_required"].(bool))
		require.False(t, v["can_start_new_attempt"].(bool))
	})
}

func businessRows(t *testing.T, rows map[string][]string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for table, values := range rows {
		out[table] = []string{}
		for _, raw := range values {
			var v map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &v))
			if table == "payment_operations" {
				delete(v, "owner_token")
				delete(v, "version")
				delete(v, "updated_at")
			}
			b, e := json.Marshal(v)
			require.NoError(t, e)
			out[table] = append(out[table], string(b))
		}
	}
	return out
}

// HTTP-007 REC-001 ID-001 DATA-003 DATA-004
func TestCallerResponseLossAfterCommittedCheckout(t *testing.T) {
	j := journey(t)
	key := uuid.NewString()
	handled := make(chan *httptest.ResponseRecorder, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saved := httptest.NewRecorder()
		j.Handler.ServeHTTP(saved, r)
		handled <- saved
		if h, ok := w.(http.Hijacker); ok {
			conn, _, e := h.Hijack()
			if e == nil {
				_ = conn.Close()
			}
		}
	}))
	defer proxy.Close()
	req, err := http.NewRequest("POST", proxy.URL+"/api/orders", strings.NewReader(purchaseBody(key, "Single café product", 2500)))
	require.NoError(t, err)
	req.SetBasicAuth("checkout-fixture-user", "checkout-fixture-password")
	req.Header.Set("Content-Type", "application/json")
	response, err := proxy.Client().Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	require.Error(t, err, "caller must lose committed response")
	select {
	case w := <-handled:
		require.Equal(t, 201, w.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("handler completion not observed")
	}
	b, err := j.Repo.LoadBinding(context.Background(), key)
	require.NoError(t, err)
	saved, err := j.Repo.LoadOrder(context.Background(), b.OrderID)
	require.NoError(t, err)
	require.Equal(t, "open", saved.Operation.State)
	require.NotNil(t, saved.Operation.SessionID)
	before := j.DB.Durable(t)
	objects := j.Stripe.LogicalObjects()
	fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	for range 2 {
		v, status := create(t, fresh, key)
		require.Equal(t, 200, status)
		id, op := envelopeIDs(t, v)
		require.Equal(t, b.OrderID, id)
		require.Equal(t, b.OperationID, op)
	}
	require.Equal(t, objects, j.Stripe.LogicalObjects())
	require.Equal(t, establishedReplayRows(t, before, b.OrderID, b.OperationID), establishedReplayRows(t, j.DB.Durable(t), b.OrderID, b.OperationID), "established replay preserves immutable data, business state, associations and exact history")
	after, err := fresh.Repo.LoadOrder(context.Background(), b.OrderID)
	require.NoError(t, err)
	require.Empty(t, after.Operation.OwnerToken, "retrieval coordination must be safely released")
	require.GreaterOrEqual(t, after.Operation.Version, saved.Operation.Version)
	require.False(t, after.Operation.UpdatedAt.Before(saved.Operation.UpdatedAt))
	require.False(t, after.Order.UpdatedAt.Before(saved.Order.UpdatedAt))
}

// STR-002 REC-001 REC-002
func TestDurableDispatchMarkerWithoutWireRecoversAfterRestart(t *testing.T) {
	j := journey(t)
	key := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway := stripeapi.New("sk_test_fixture", stripeapi.Options{BackendURL: j.Stripe.Server.URL, HTTPClient: j.Stripe.Server.Client()})
	require.NotNil(t, gateway)
	var marker payment.View
	interrupted := &interruptGateway{Gateway: gateway, before: func(callctx context.Context, snapshot payment.Snapshot) error {
		var err error
		marker, err = j.Repo.LoadOrder(context.Background(), snapshot.OrderID)
		require.NoError(t, err)
		require.Equal(t, "unresolved", marker.Operation.State)
		require.NotNil(t, marker.Operation.FirstDispatchAt)
		require.NotNil(t, marker.Operation.LastDispatchAt)
		require.Equal(t, snapshot, marker.Operation.Snapshot)
		require.Empty(t, j.Stripe.Wires())
		cancel()
		return callctx.Err()
	}}
	service := payment.New(j.Repo, interrupted, j.Options)
	require.NotNil(t, service)
	_, _ = service.Create(ctx, createInput(key))
	require.NotEmpty(t, marker.Order.ID)
	committed, err := j.Repo.LoadOrder(context.Background(), marker.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "unresolved", committed.Operation.State)
	require.Nil(t, committed.Operation.SessionID)
	require.Empty(t, j.Stripe.Wires())
	independent := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	recovered, err := independent.Service.Create(context.Background(), createInput(key))
	require.NoError(t, err)
	require.True(t, recovered.Established)
	require.Equal(t, marker.Operation.ID, recovered.Operation.ID)
	require.Equal(t, marker.Operation.StripeKey, recovered.Operation.StripeKey)
	require.Equal(t, testutil.ImmutableSnapshot(marker.Operation.Snapshot), testutil.ImmutableSnapshot(recovered.Operation.Snapshot))
	require.Equal(t, 1, j.Stripe.LogicalObjects())
	require.Len(t, j.Stripe.Wires(), 1)
	testutil.CheckWire(t, j.Stripe.Wires()[0], marker.Operation.Snapshot)
}

type interruptGateway struct {
	payment.Gateway
	before func(context.Context, payment.Snapshot) error
}

func (g *interruptGateway) Create(ctx context.Context, s payment.Snapshot) (payment.SessionEvidence, error) {
	if err := g.before(ctx, s); err != nil {
		return payment.SessionEvidence{ErrorClass: "transport"}, err
	}
	return g.Gateway.Create(ctx, s)
}

// HTTP-007 REC-002
func TestResultAndReadUnavailableReturnKnownIdentity(t *testing.T) {
	j := journey(t)
	key := uuid.NewString()
	barrier := testutil.NewBarrier()
	defer barrier.Release()
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		j.Handler.ServeHTTP(w, testutil.Request("POST", "/api/orders", purchaseBody(key, "Single café product", 2500), true))
		done <- w
	}()
	select {
	case <-barrier.Arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("SDK success boundary not reached")
	}
	binding, err := j.Repo.LoadBinding(context.Background(), key)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	timeout := time.Second
	require.NoError(t, j.DB.Container.Stop(ctx, &timeout))
	barrier.Release()
	var w *httptest.ResponseRecorder
	select {
	case w = <-done:
	case <-time.After(12 * time.Second):
		t.Fatal("unavailable result/read not bounded")
	}
	require.Equal(t, 503, w.Code)
	e := testutil.JSON(t, w)["error"].(map[string]any)
	require.Equal(t, "temporarily_unavailable", e["code"])
	require.Equal(t, binding.OrderID, e["order_id"])
	require.Equal(t, binding.OperationID, e["operation_id"])
	requireMeaningfulLog(t, j.Logs, w.Header().Get("X-Request-ID"), binding.OrderID, binding.OperationID, w.Code)
	require.Equal(t, 1, j.Stripe.LogicalObjects())
	require.NoError(t, j.DB.Container.Start(ctx))
	require.Eventually(t, func() bool { return j.DB.Pool.Ping(ctx) == nil }, 15*time.Second, 100*time.Millisecond)
	fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
	j.Stripe.Configure(func(s *testutil.StripeServer) { s.Before = nil })
	v, status := create(t, fresh, key)
	require.Equal(t, 200, status)
	id, op := envelopeIDs(t, v)
	require.Equal(t, binding.OrderID, id)
	require.Equal(t, binding.OperationID, op)
	require.Equal(t, 1, j.Stripe.LogicalObjects())
}

// REC-001 STR-003
func TestRecoveryWithOptionalSupplementalStripeRequestIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		omit bool
	}{{"request_id_absent", true}, {"request_id_present", false}} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			key := uuid.NewString()
			j.Stripe.SetScript(func(testutil.Wire) testutil.StripeReply {
				return testutil.StripeReply{Drop: true, OmitRequestID: tc.omit}
			})
			v, status := create(t, j, key)
			require.Equal(t, 202, status)
			id, op := envelopeIDs(t, v)
			j.Stripe.SetScript(func(testutil.Wire) testutil.StripeReply { return testutil.StripeReply{OmitRequestID: tc.omit} })
			fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
			v, status = create(t, fresh, key)
			require.Equal(t, 200, status)
			id2, op2 := envelopeIDs(t, v)
			require.Equal(t, id, id2)
			require.Equal(t, op, op2)
			require.Equal(t, 1, j.Stripe.LogicalObjects())
			history, err := fresh.Repo.ReadHistory(context.Background(), id, 0, 100)
			require.NoError(t, err)
			found := false
			for _, h := range history.Entries {
				if h.Kind == "operation_state_changed" && h.ToState != nil && *h.ToState == "open" {
					found = true
					if tc.omit {
						require.Nil(t, h.RequestID)
					} else {
						require.Equal(t, "req_test_checkout", *h.RequestID)
					}
				}
			}
			require.True(t, found)
		})
	}
}

// STR-002 DATA-001
func TestFailedDispatchCommitSendsNoWire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		commit bool
	}{{"history_write", false}, {"history_commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			key := uuid.NewString()
			release := testutil.FailDispatchHistory(t, j.DB.Independent(t), tc.commit)
			v, status := create(t, j, key)
			require.Contains(t, []int{202, 503}, status)
			require.Empty(t, j.Stripe.Wires())
			binding, err := j.Repo.LoadBinding(context.Background(), key)
			require.NoError(t, err)
			saved, err := j.Repo.LoadOrder(context.Background(), binding.OrderID)
			require.NoError(t, err)
			require.Equal(t, "prepared", saved.Operation.State)
			require.Nil(t, saved.Operation.FirstDispatchAt)
			require.Nil(t, saved.Operation.LastDispatchAt)
			require.Equal(t, 1, j.DB.Counts(t)["payment_operations"])
			if status == 202 {
				id, op := envelopeIDs(t, v)
				require.Equal(t, binding.OrderID, id)
				require.Equal(t, binding.OperationID, op)
			}
			release()
			fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
			v, status = create(t, fresh, key)
			require.Equal(t, 200, status)
			id, op := envelopeIDs(t, v)
			require.Equal(t, binding.OrderID, id)
			require.Equal(t, binding.OperationID, op)
			require.Equal(t, 1, j.Stripe.LogicalObjects())
		})
	}
}

// establishedReplayRows permits bookkeeping only on the observed order/operation.
// Every other row and the complete append-only history stay byte-equivalent in
// content. Failed transactions and stale/unauthorized paths use full Durable rows.
func establishedReplayRows(t *testing.T, rows map[string][]string, orderID, operationID string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for table, values := range rows {
		out[table] = []string{}
		for _, raw := range values {
			var row map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &row))
			if table == "payment_orders" && row["id"] == orderID {
				delete(row, "updated_at")
				delete(row, "version")
			}
			if table == "payment_operations" && row["id"] == operationID {
				for _, field := range []string{"owner_token", "version", "updated_at", "observed_at", "last_observed_at", "evidence_source"} {
					delete(row, field)
				}
			}
			encoded, err := json.Marshal(row)
			require.NoError(t, err)
			out[table] = append(out[table], string(encoded))
		}
	}
	return out
}
