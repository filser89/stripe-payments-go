package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/stripeapi"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type checkoutJourney struct {
	DB      *testutil.DB
	Repo    payment.Repository
	Service *payment.Service
	Stripe  *testutil.StripeServer
	Handler http.Handler
	Logs    *testutil.Logs
	Options payment.Options
	Server  *web.Server
}

func journey(t *testing.T) *checkoutJourney {
	t.Helper()
	db := testutil.Database(t)
	return journeyWith(t, db, db.Pool, testutil.NewStripeServer(t), testutil.Options())
}
func journeyWith(t *testing.T, db *testutil.DB, pool *pgxpool.Pool, s *testutil.StripeServer, options payment.Options) *checkoutJourney {
	t.Helper()
	repo := postgres.NewPaymentRepository(pool)
	require.NotNil(t, repo, "missing real payment persistence boundary")
	db.Relations(t)
	gateway := stripeapi.New("sk_test_fixture", stripeapi.Options{BackendURL: s.Server.URL, HTTPClient: s.Server.Client()})
	require.NotNil(t, gateway, "missing real Stripe SDK boundary")
	logs := &testutil.Logs{}
	options.Logger = logs.Logger()
	service := payment.New(repo, gateway, options)
	require.NotNil(t, service, "missing payment operations")
	extra := web.NewCheckoutHandler(service, options.RequestTimeout, logs.Logger())
	require.NotNil(t, extra, "missing feature HTTP boundary")
	env := testutil.Environment(db.URL, "localhost:8080")
	env["PAYMENT_CURRENCY"] = options.Currency
	env["PAYMENT_MIN_AMOUNT"] = fmt.Sprint(options.MinAmount)
	env["PAYMENT_MAX_AMOUNT"] = fmt.Sprint(options.MaxAmount)
	env["APP_BASE_URL"] = options.Origin
	env["CHECKOUT_REQUEST_TIMEOUT"] = options.RequestTimeout.String()
	env["STRIPE_CALL_TIMEOUT"] = options.CallTimeout.String()
	env["STRIPE_RETRY_BUDGET"] = options.RetryBudget.String()
	env["STRIPE_MAX_ATTEMPTS"] = fmt.Sprint(options.MaxAttempts)
	env["HTTP_READ_TIMEOUT"] = (options.RequestTimeout + time.Second).String()
	env["HTTP_WRITE_TIMEOUT"] = (options.RequestTimeout + time.Second).String()

	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NoError(t, c.ValidateServing(), "connected witness uses valid serving settings")
	require.Equal(t, options.RequestTimeout, c.CheckoutSettings().RequestTimeout)
	server := web.New(c, logs.Logger(), pool.Ping, extra)
	return &checkoutJourney{db, repo, service, s, server.Handler, logs, options, server}
}
func purchaseBody(key, description string, amount int64) string {
	b, _ := json.Marshal(map[string]any{"request_key": key, "description": description, "amount": amount})
	return string(b)
}
func create(t *testing.T, j *checkoutJourney, key string) (map[string]any, int) {
	t.Helper()
	w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(key, "Single café product", 2500), true))
	return testutil.JSON(t, w), w.Code
}
func envelopeIDs(t *testing.T, v map[string]any) (string, string) {
	t.Helper()
	return v["order"].(map[string]any)["id"].(string), v["operation"].(map[string]any)["id"].(string)
}
func TestConnectedOrderCheckoutAndInspection(t *testing.T) { // HTTP-001 HTTP-002 HTTP-003 HTTP-004 HTTP-005 HTTP-006 CFG-001 ID-001 ID-002 ID-003 ID-004 DATA-004 SEC-002 SEC-003
	t.Run("create_replay_read_history_and_limits", func(t *testing.T) {
		j := journey(t)
		key := uuid.NewString()
		v, status := create(t, j, key)
		require.Equal(t, 201, status)
		orderID, opID := envelopeIDs(t, v)
		require.NotEqual(t, key, orderID)
		require.NotEqual(t, orderID, opID)
		for _, id := range []string{key, orderID, opID} {
			parsed, e := uuid.Parse(id)
			require.NoError(t, e)
			require.Equal(t, uuid.Version(4), parsed.Version())
		}
		require.Equal(t, "unpaid", v["order"].(map[string]any)["payment_status"])
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		require.Len(t, j.Stripe.Wires(), 1)
		binding, err := j.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		require.Equal(t, opID, binding.OperationID)
		persisted, err := j.Repo.LoadOrder(context.Background(), orderID)
		require.NoError(t, err)
		require.NotEqual(t, key, persisted.Operation.StripeKey)
		require.LessOrEqual(t, len(persisted.Operation.StripeKey), 255)
		testutil.CheckWire(t, j.Stripe.Wires()[0], persisted.Operation.Snapshot)
		before := j.DB.Counts(t)
		replayed, status := create(t, j, key)
		require.Equal(t, 200, status)
		require.Equal(t, orderID, replayed["order"].(map[string]any)["id"])
		require.Equal(t, opID, replayed["operation"].(map[string]any)["id"])
		require.Equal(t, before, j.DB.Counts(t))
		for _, path := range []string{"/api/orders/" + orderID, "/api/orders/" + orderID + "/history", "/api/orders/" + orderID + "/history?after=0&limit=1"} {
			wireBefore := len(j.Stripe.Wires())
			w := testutil.Response(t, j.Handler, testutil.Request("GET", path, "", true))
			require.Equal(t, 200, w.Code)
			require.Equal(t, before, j.DB.Counts(t))
			require.Equal(t, wireBefore, len(j.Stripe.Wires()))
			head := testutil.Response(t, j.Handler, testutil.Request("HEAD", path, "", true))
			require.Equal(t, w.Code, head.Code)
			require.Empty(t, head.Body.String())
			require.Equal(t, w.Header().Get("Cache-Control"), head.Header().Get("Cache-Control"))
		}
		tight := j.Options
		tight.MinAmount = 5000
		tight.MaxAmount = 6000
		restart := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, tight)
		replayed, status = create(t, restart, key)
		require.Equal(t, 200, status)
		require.Equal(t, float64(2500), replayed["order"].(map[string]any)["amount"])
		w := testutil.Response(t, restart.Handler, testutil.Request("POST", "/api/orders", purchaseBody(key, "changed", 2500), true))
		require.Equal(t, 409, w.Code)
		require.Contains(t, w.Body.String(), "idempotency_conflict")
		w = testutil.Response(t, restart.Handler, testutil.Request("POST", "/api/orders", purchaseBody(uuid.NewString(), "new", 2500), true))
		require.Equal(t, 400, w.Code)
		second, status := create(t, j, uuid.NewString())
		require.Equal(t, 201, status)
		secondID, _ := envelopeIDs(t, second)
		require.NotEqual(t, orderID, secondID)
		require.Equal(t, 2, j.Stripe.LogicalObjects())
		for _, sentinel := range []string{"sk_test_fixture", "checkout-fixture-password", "test-only-fixture"} {
			require.NotContains(t, j.Logs.Contents(), sentinel)
		}
	})
	t.Run("unavailable_checkout_remains_inspectable", func(t *testing.T) {
		j := journey(t)
		j.Stripe.Configure(func(s *testutil.StripeServer) { s.Status = 500 })
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.ErrorBody = `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`
		})
		v, status := create(t, j, uuid.NewString())
		require.Equal(t, 202, status)
		id, op := envelopeIDs(t, v)
		require.NotEmpty(t, id)
		require.NotEmpty(t, op)
		require.LessOrEqual(t, len(j.Stripe.Wires()), 3)
		view, err := j.Repo.LoadOrder(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, "unpaid", view.Order.Status)
		require.Equal(t, "unresolved", view.Operation.State)
		require.Nil(t, view.Operation.SessionID)
		w := testutil.Response(t, j.Handler, testutil.Request("GET", "/api/orders/"+id, "", true))
		require.Equal(t, 200, w.Code)
		require.NotContains(t, w.Body.String(), "sensitive-sentinel")
		require.NotContains(t, j.Logs.Contents(), "sensitive-sentinel")
	})
}
func TestConnectedInputAndAuthenticationEffects(t *testing.T) { // INP-001 INP-002 INP-003 INP-004 INP-005 INP-006 SEC-001 SEC-003 HTTP-004 HTTP-005
	for _, tc := range []struct {
		name, method, path, body, media string
		auth                            bool
		status                          int
	}{
		{"invalid_description", "POST", "/api/orders", `{"description":" item","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json", true, 400},
		{"amount_exponent", "POST", "/api/orders", `{"description":"item","amount":25e2,"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json", true, 400},
		{"key_variant", "POST", "/api/orders", `{"description":"item","amount":2500,"request_key":"11111111-1111-4111-1111-111111111111"}`, "application/json", true, 400},
		{"duplicate_field", "POST", "/api/orders", `{"description":"item","amount":2500,"amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json", true, 400},
		{"unsupported_media", "POST", "/api/orders", `{}`, "text/plain", true, 415},
		{"oversize", "POST", "/api/orders", strings.Repeat(" ", 4097), "application/json", true, 413},
		{"read_body", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", "x", "application/json", true, 400},
		{"history_query", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=-1", "", "application/json", true, 400},
		{"unknown_order", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", "", "application/json", true, 404},
		{"wrong_method", "DELETE", "/api/orders", "", "application/json", true, 405},
		{"initial_unauthorized", "POST", "/api/orders", `{broken`, "application/json", false, 401},
		{"checkout_unauthorized", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/checkout", `{broken`, "application/json", false, 401},
		{"read_unauthorized", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", "x", "application/json", false, 401},
		{"history_unauthorized", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history", "x", "application/json", false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			before := j.DB.Counts(t)
			req := testutil.Request(tc.method, tc.path, tc.body, tc.auth)
			req.Header.Set("Content-Type", tc.media)
			w := testutil.Response(t, j.Handler, req)
			require.Equal(t, tc.status, w.Code)
			testutil.RequireRejectedLog(t, j.Logs, w.Header().Get("X-Request-ID"), w.Code, "", "", tc.body)
			require.Equal(t, before, j.DB.Counts(t))
			require.Empty(t, j.Stripe.Wires())
			if tc.body != "" {
				require.NotContains(t, w.Body.String(), tc.body, "input must not be echoed")
			}
		})
	}
}

// INP-001 INP-002 CFG-001 STR-001 HTTP-002
func TestConnectedAcceptedPurchasePreservation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		amount            int64
	}{
		{"lower_bound", "ÉCOLE  Café e\u0301", 500}, {"upper_bound", "École  café é", 1000}, {"canonical_distinct", "e\u0301  Product", 750},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			s := testutil.NewStripeServer(t)
			opts := testutil.Options()
			opts.MinAmount = 500
			opts.MaxAmount = 1000
			opts.Origin = "http://127.0.0.1:9090"
			j := journeyWith(t, db, db.Pool, s, opts)
			key := uuid.NewString()
			w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(key, tc.description, tc.amount), true))
			require.Equal(t, 201, w.Code)
			v := testutil.JSON(t, w)
			id, _ := envelopeIDs(t, v)
			o := v["order"].(map[string]any)
			require.Equal(t, tc.description, o["description"])
			require.Equal(t, float64(tc.amount), o["amount"])
			require.Equal(t, "usd", o["currency"])
			stored, err := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, tc.description, stored.Order.Description)
			require.Equal(t, tc.amount, stored.Order.Amount)
			binding, err := j.Repo.LoadBinding(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, tc.description, binding.Description)
			require.Equal(t, tc.amount, binding.Amount)
			require.Len(t, s.Wires(), 1)
			wire := s.Wires()[0]
			require.Equal(t, tc.description, wire.Form.Get("line_items[0][price_data][product_data][name]"))
			require.Equal(t, fmt.Sprint(tc.amount), wire.Form.Get("line_items[0][price_data][unit_amount]"))
			require.Equal(t, opts.Origin+"/?order_id="+id+"&checkout_return=success", wire.Form.Get("success_url"))
			require.Equal(t, opts.Origin+"/?order_id="+id+"&checkout_return=cancel", wire.Form.Get("cancel_url"))
		})
	}
}

// HTTP-006 DATA-004
func TestConnectedHistoryValuesPaginationAndIsolation(t *testing.T) {
	j := journey(t)
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	id, _ := envelopeIDs(t, v)
	other, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	otherID, _ := envelopeIDs(t, other)
	page, err := j.Repo.ReadHistory(context.Background(), id, 0, 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(page.Entries), 3)
	before := j.DB.Durable(t)
	wires := len(j.Stripe.Wires())
	after := int64(0)
	seen := []payment.HistoryEntry{}
	for _, expected := range page.Entries {
		w := testutil.Response(t, j.Handler, testutil.Request("GET", fmt.Sprintf("/api/orders/%s/history?after=%d&limit=1", id, after), "", true))
		require.Equal(t, 200, w.Code)
		body := testutil.JSON(t, w)
		require.Equal(t, id, body["order_id"])
		entries := body["entries"].([]any)
		require.Len(t, entries, 1)
		entry := entries[0].(map[string]any)
		require.Equal(t, float64(expected.Sequence), entry["sequence"])
		require.Equal(t, expected.Kind, entry["kind"])
		require.Equal(t, expected.OrderID, entry["order_id"])
		require.NotEqual(t, otherID, entry["order_id"])
		recorded, err := time.Parse(time.RFC3339Nano, entry["recorded_at"].(string))
		require.NoError(t, err)
		require.True(t, recorded.Equal(expected.ObservedAt))
		require.Equal(t, 0, utcOffset(recorded))
		values := map[string]*string{"operation_id": testutil.Pointer(expected.OperationID), "from_state": expected.FromState, "to_state": expected.ToState, "stripe_session_id": expected.SessionID, "stripe_payment_intent_id": expected.PaymentIntentID, "stripe_event_id": expected.EventID, "stripe_request_id": expected.RequestID, "failure_code": expected.FailureCode}
		for key, p := range values {
			if p == nil || *p == "" {
				require.Nil(t, entry[key], key)
			} else {
				require.Equal(t, *p, entry[key], key)
			}
		}
		require.Equal(t, float64(expected.Sequence), body["next_after"])
		after = expected.Sequence
		seen = append(seen, expected)
	}
	require.Equal(t, page.Entries, seen)
	empty := testutil.Response(t, j.Handler, testutil.Request("GET", fmt.Sprintf("/api/orders/%s/history?after=%d&limit=1", id, after), "", true))
	body := testutil.JSON(t, empty)
	require.Empty(t, body["entries"])
	require.Equal(t, float64(after), body["next_after"])
	require.Equal(t, before, j.DB.Durable(t))
	require.Equal(t, wires, len(j.Stripe.Wires()))
}
func utcOffset(t time.Time) int { _, n := t.Zone(); return n }

// SEC-001 SEC-003
func TestConnectedFeatureCredentialIsolation(t *testing.T) {
	j := journey(t)
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 201, status)
	id, op := envelopeIDs(t, v)
	for _, endpoint := range []struct{ method, path, body string }{
		{"POST", "/api/orders", purchaseBody(uuid.NewString(), "isolated", 2500)},
		{"POST", "/api/orders/" + id + "/checkout", `{"request_key":"` + uuid.NewString() + `"}`},
		{"GET", "/api/orders/" + id, ""}, {"HEAD", "/api/orders/" + id, ""}, {"GET", "/api/orders/" + id + "/history", ""}, {"HEAD", "/api/orders/" + id + "/history", ""},
	} {
		for _, credential := range []string{"missing", "wrong"} {
			before := j.DB.Durable(t)
			wireBefore := len(j.Stripe.Wires())
			r := testutil.Request(endpoint.method, endpoint.path, endpoint.body, false)
			spy := &readSpy{Reader: strings.NewReader(endpoint.body)}
			r.Body = spy
			if credential == "wrong" {
				r.SetBasicAuth("checkout-fixture-user", "wrong-password")
			}
			w := testutil.Response(t, j.Handler, r)
			require.Equal(t, 401, w.Code)
			testutil.RequireRejectedLog(t, j.Logs, w.Header().Get("X-Request-ID"), w.Code, id, op)
			require.Zero(t, spy.reads)
			require.Equal(t, before, j.DB.Durable(t))
			require.Equal(t, wireBefore, len(j.Stripe.Wires()))
		}
	}
	// One valid checkout remains admitted while wrong/missing feature requests overlap.
	barrier := testutil.NewBarrier()
	defer barrier.Release()
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Before = func(ctx context.Context, w testutil.Wire) {
			if w.Method == "GET" {
				_ = barrier.Wait(ctx)
			}
		}
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		j.Handler.ServeHTTP(w, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+uuid.NewString()+`"}`, true))
		done <- w
	}()
	select {
	case <-barrier.Arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("authenticated checkout did not overlap")
	}
	before := j.DB.Durable(t)
	wireBefore := len(j.Stripe.Wires())
	for _, method := range []string{"GET", "HEAD"} {
		r := testutil.Request(method, "/api/orders/"+id+"/history", "malformed", false)
		r.SetBasicAuth("checkout-fixture-user", "wrong")
		spy := &readSpy{Reader: strings.NewReader("malformed")}
		r.Body = spy
		w := testutil.Response(t, j.Handler, r)
		require.Equal(t, 401, w.Code)
		testutil.RequireRejectedLog(t, j.Logs, w.Header().Get("X-Request-ID"), w.Code, id, op)
		require.Zero(t, spy.reads)
	}
	require.Equal(t, before, j.DB.Durable(t))
	require.Equal(t, wireBefore, len(j.Stripe.Wires()))
	barrier.Release()
	select {
	case w := <-done:
		require.Equal(t, 200, w.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("authenticated checkout did not join")
	}
}

type readSpy struct {
	io.Reader
	reads int
}

func (s *readSpy) Read(p []byte) (int, error) { s.reads++; return s.Reader.Read(p) }
func (*readSpy) Close() error                 { return nil }

func requireMeaningfulLog(t *testing.T, logs *testutil.Logs, requestID, orderID, operationID string, status int) {
	t.Helper()
	records, err := logs.Records()
	require.NoError(t, err)
	found := false
	for _, record := range records {
		if orderID != "" && record["order_id"] != orderID {
			continue
		}
		if operationID != "" && record["operation_id"] != operationID {
			continue
		}
		if requestID != "" && record["request_id"] != requestID {
			continue
		}
		if testutil.OutcomeLogContext(record, status) {
			found = true
		}
	}
	require.True(t, found, "correlated log must carry useful structured outcome/error context")
	for _, sentinel := range []string{"sk_test_fixture", "checkout-fixture-password", "test-only-fixture", "sensitive-sentinel", "4242424242424242", "CVC_SENTINEL"} {
		require.NotContains(t, logs.Contents(), sentinel)
	}
}

// SEC-003 SEC-002
func TestConnectedUsefulSanitizedCheckoutOutcomeLogs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reply  testutil.StripeReply
	}{
		{"accepted", 201, testutil.StripeReply{}},
		{"confirmed_rejection", 502, testutil.StripeReply{Status: 400, Body: `{"error":{"type":"invalid_request_error","code":"parameter_invalid_integer","param":"line_items","message":"sensitive-sentinel"}}`}},
		{"server_pending", 202, testutil.StripeReply{Status: 500, Body: `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`}},
		{"mismatched_correlation", 202, testutil.StripeReply{Mutation: func(o map[string]any) { o["client_reference_id"] = "wrong" }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			j.Stripe.SetScript(func(testutil.Wire) testutil.StripeReply { return tc.reply })
			w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(uuid.NewString(), "Log product", 2500), true))
			require.Equal(t, tc.status, w.Code)
			var id, op string
			body := testutil.JSON(t, w)
			if tc.status == 502 {
				e := body["error"].(map[string]any)
				id = e["order_id"].(string)
				op = e["operation_id"].(string)
			} else {
				id, op = envelopeIDs(t, body)
			}
			requireMeaningfulLog(t, j.Logs, w.Header().Get("X-Request-ID"), id, op, w.Code)
		})
	}
	t.Run("stripe_timeout", func(t *testing.T) {
		j := journey(t)
		barrier := testutil.NewBarrier()
		defer barrier.Release()
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		})
		opts := j.Options
		opts.CallTimeout = 100 * time.Millisecond
		opts.MaxAttempts = 1
		j = journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, opts)
		w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(uuid.NewString(), "Timeout product", 2500), true))
		require.Equal(t, 202, w.Code)
		id, op := envelopeIDs(t, testutil.JSON(t, w))
		requireMeaningfulLog(t, j.Logs, w.Header().Get("X-Request-ID"), id, op, w.Code)
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("timeout peer not joined")
		}
	})
	t.Run("database_acceptance_failure", func(t *testing.T) {
		j := journey(t)
		release := testutil.FailHistory(t, j.DB.Independent(t), true)
		defer release()
		w := testutil.Response(t, j.Handler, testutil.Request("POST", "/api/orders", purchaseBody(uuid.NewString(), "Database product", 2500), true))
		require.Equal(t, 503, w.Code)
		requireMeaningfulLog(t, j.Logs, w.Header().Get("X-Request-ID"), "", "", w.Code)
	})
}
