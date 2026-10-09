package integration_test

import (
	"context"
	"encoding/json"
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/stripeapi"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
)

type checkoutJourney struct {
	DB      *testutil.DB
	Repo    payment.Repository
	Service *payment.Service
	Stripe  *testutil.StripeServer
	Handler http.Handler
	Logs    *testutil.Logs
	Options payment.Options
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
	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	c.ReadTimeout = options.RequestTimeout + 1e9
	c.WriteTimeout = c.ReadTimeout
	server := web.New(c, logs.Logger(), pool.Ping, extra)
	return &checkoutJourney{db, repo, service, s, server.Handler, logs, options}
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
		j.Stripe.Status = 500
		j.Stripe.ErrorBody = `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`
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
func TestConnectedInputAndAuthenticationEffects(t *testing.T) { // INP-001 INP-002 INP-003 INP-004 INP-005 INP-006 SEC-001 HTTP-004 HTTP-005
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
			require.Equal(t, before, j.DB.Counts(t))
			require.Empty(t, j.Stripe.Wires())
			if tc.body != "" {
				require.NotContains(t, w.Body.String(), tc.body, "input must not be echoed")
			}
		})
	}
}
