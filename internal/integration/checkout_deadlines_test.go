package integration_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/postgres"
	"github.com/filser89/stripe-payments-go/internal/stripeapi"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnectedCheckoutBudgetsAndShutdown(t *testing.T) { // STR-004 STR-005 STR-006 STR-007 LIFE-009 FND-002 FND-003 FND-004
	for _, tc := range []struct {
		name     string
		attempts int
	}{{"one_wire", 1}, {"two_wires", 2}, {"three_wires", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			s := testutil.NewStripeServer(t)
			s.Configure(func(s *testutil.StripeServer) {
				s.Status = 500
				s.ErrorBody = `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`
			})
			options := testutil.Options()
			options.MaxAttempts = tc.attempts
			j := journeyWith(t, db, db.Pool, s, options)
			start := time.Now()
			v, status := create(t, j, uuid.NewString())
			require.Equal(t, 202, status)
			require.Len(t, s.Wires(), tc.attempts, "SDK retries and business calls share wire ceiling")
			require.Less(t, time.Since(start), options.RequestTimeout+time.Second)
			id, _ := envelopeIDs(t, v)
			view, err := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, "unresolved", view.Operation.State)
			require.Equal(t, "unpaid", view.Order.Status)
			wires := s.Wires()
			for n := 1; n < len(wires); n++ {
				minimum := 250 * time.Millisecond
				if n == 2 {
					minimum = 500 * time.Millisecond
				}
				require.GreaterOrEqual(t, wires[n].At.Sub(wires[n-1].At), minimum-20*time.Millisecond)
				require.Equal(t, wires[0].Header.Get("Idempotency-Key"), wires[n].Header.Get("Idempotency-Key"))
				require.Equal(t, wires[0].Form, wires[n].Form)
			}
		})
	}
	t.Run("blocked_wire_caller_cancel_joins_and_retains_identity", func(t *testing.T) {
		j := journey(t)
		barrier := testutil.NewBarrier()
		j.Stripe.Configure(func(s *testutil.StripeServer) {
			s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		})
		defer barrier.Release()
		key := uuid.NewString()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := j.Service.Create(ctx, createInput(key)); done <- err }()
		select {
		case <-barrier.Arrived:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("wire not admitted")
		}
		binding, err := j.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, err)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("payment owned work not joined")
		}
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("SDK peer remained blocked")
		}
		view, err := j.Repo.LoadOrder(context.Background(), binding.OrderID)
		require.NoError(t, err)
		require.NotEqual(t, "rejected", view.Operation.State)
		require.Equal(t, binding.OperationID, view.Operation.ID)
		require.NotEmpty(t, view.Operation.StripeKey)
		require.NotNil(t, view.Operation.FirstDispatchAt)
		requireMeaningfulLog(t, j.Logs, "", binding.OrderID, binding.OperationID, 0)
	})
}

func createInput(key string) payment.CreateInput {
	return payment.CreateInput{Description: "Single café product", Amount: 2500, RequestKey: key}
}

// STR-004 FND-004 DATA-001
func TestConnectedConfiguredDeadlineIncludesRealDatabaseWaits(t *testing.T) {
	for _, tc := range []struct {
		name         string
		afterSuccess bool
		parent       bool
	}{
		{"before_dispatch", false, false}, {"after_success", true, false}, {"earlier_parent", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			s := testutil.NewStripeServer(t)
			opts := testutil.Options()
			opts.RequestTimeout = 2 * time.Second
			opts.CallTimeout = 500 * time.Millisecond
			opts.RetryBudget = time.Second
			opts.MaxAttempts = 1
			j := journeyWith(t, db, db.Pool, s, opts)
			i := testutil.Intent()
			_, err := j.Repo.AcceptInitial(context.Background(), i)
			require.NoError(t, err)
			control := db.Independent(t)
			var release func()
			barrier := testutil.NewBarrier()
			defer barrier.Release()
			if tc.afterSuccess {
				s.Configure(func(s *testutil.StripeServer) {
					s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
				})
			} else {
				release = testutil.HoldOrder(t, control, i.Order.ID)
				defer release()
			}
			ctx := context.Background()
			cancel := func() {}
			if tc.parent {
				ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
			}
			defer cancel()
			req := testutil.Request("POST", "/api/orders/"+i.Order.ID+"/checkout", `{"request_key":"`+uuid.NewString()+`"}`, true).WithContext(ctx)
			done := make(chan *httptest.ResponseRecorder, 1)
			start := time.Now()
			go func() { w := httptest.NewRecorder(); j.Handler.ServeHTTP(w, req); done <- w }()
			if tc.afterSuccess {
				select {
				case <-barrier.Arrived:
				case <-time.After(time.Second):
					t.Fatal("success call not observed")
				}
				release = testutil.HoldOrder(t, control, i.Order.ID)
				defer release()
				barrier.Release()
			}
			require.Eventually(t, func() bool {
				var n int
				e := control.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&n)
				return e == nil && n >= 1
			}, time.Second, 10*time.Millisecond, "real DB work must be observed waiting")
			var response *httptest.ResponseRecorder
			select {
			case response = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("database work ignores admission deadline")
			}
			maximum := opts.RequestTimeout + 500*time.Millisecond
			if tc.parent {
				maximum = 800 * time.Millisecond
			}
			require.Less(t, time.Since(start), maximum)
			require.NotEqual(t, 201, response.Code, "uncommitted blocked work cannot claim success")
			release()
			wireCount := len(s.Wires())
			if !tc.afterSuccess {
				require.Zero(t, wireCount)
			} else {
				require.Equal(t, 1, wireCount)
				require.Equal(t, 1, s.LogicalObjects(), "external success exists before blocked result persistence")
			}
			view, err := j.Repo.LoadOrder(context.Background(), i.Order.ID)
			require.NoError(t, err)
			require.Equal(t, i.Operation.ID, view.Operation.ID)
			require.Equal(t, "unpaid", view.Order.Status)
			require.Nil(t, view.Operation.SessionID)
			require.Equal(t, wireCount, len(s.Wires()), "no detached work after handler join")
		})
	}
}

// STR-005 STR-006 LIFE-009
func TestConnectedMixedWireBudgetsAndPreparedRecovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
	}{{"one_total", 1}, {"two_total", 2}, {"three_total", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			v, status := create(t, j, uuid.NewString())
			require.Equal(t, 201, status)
			id, old := envelopeIDs(t, v)
			j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
				if w.Method == "GET" {
					return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["payment_status"] = "unpaid"; o["url"] = "" }}
				}
				return testutil.StripeReply{Status: 500, Body: `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`}
			})
			opts := j.Options
			opts.MaxAttempts = tc.attempts
			continued := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, opts)
			wireBefore := len(j.Stripe.Wires())
			key := uuid.NewString()
			w := testutil.Response(t, continued.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			require.Equal(t, 202, w.Code)
			out := testutil.JSON(t, w)
			_, newOp := envelopeIDs(t, out)
			require.NotEqual(t, old, newOp)
			wires := j.Stripe.Wires()[wireBefore:]
			require.Len(t, wires, tc.attempts)
			require.Equal(t, "GET", wires[0].Method)
			for n := 1; n < len(wires); n++ {
				require.Equal(t, "POST", wires[n].Method)
				if n > 1 {
					require.Equal(t, wires[1].Header.Get("Idempotency-Key"), wires[n].Header.Get("Idempotency-Key"))
					require.Equal(t, wires[1].Form, wires[n].Form)
				}
			}
			prepared, err := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, newOp, prepared.Operation.ID)
			if tc.attempts == 1 {
				require.Equal(t, "prepared", prepared.Operation.State)
				require.Nil(t, prepared.Operation.FirstDispatchAt)
			}
			binding, err := j.Repo.LoadBinding(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, newOp, binding.OperationID)
			history, err := j.Repo.ReadHistory(context.Background(), id, 0, 100)
			require.NoError(t, err)
			preparedEntries := 0
			for _, h := range history.Entries {
				if h.OperationID == newOp && h.Kind == "operation_prepared" {
					preparedEntries++
				}
			}
			require.Equal(t, 1, preparedEntries)
			j.Stripe.SetScript(nil)
			fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
			w = testutil.Response(t, fresh.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			require.Equal(t, 200, w.Code)
			_, recovered := envelopeIDs(t, testutil.JSON(t, w))
			require.Equal(t, newOp, recovered)
			require.Equal(t, 2, j.DB.Counts(t)["payment_operations"])
			require.Equal(t, 2, j.Stripe.LogicalObjects())
			last := j.Stripe.Wires()[len(j.Stripe.Wires())-1]
			require.Equal(t, prepared.Operation.StripeKey, last.Header.Get("Idempotency-Key"))
			testutil.CheckWire(t, last, func() payment.Snapshot {
				saved, e := fresh.Repo.LoadOrder(context.Background(), id)
				require.NoError(t, e)
				return saved.Operation.Snapshot
			}())
		})
	}
}

// STR-005 STR-006 STR-007
func TestConnectedActualRetryHeadersAndWaitCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, retry, should string
		budget              time.Duration
		calls               int
		minimum             time.Duration
	}{
		{"short_header", "0", "", 2 * time.Second, 3, 250 * time.Millisecond},
		{"long_header", "1", "", 3 * time.Second, 3, time.Second},
		{"cannot_fit", "2", "", 500 * time.Millisecond, 1, 0},
		{"should_retry_false", "", "false", 2 * time.Second, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.Database(t)
			s := testutil.NewStripeServer(t)
			s.SetScript(func(testutil.Wire) testutil.StripeReply {
				h := http.Header{}
				if tc.retry != "" {
					h.Set("Retry-After", tc.retry)
				}
				if tc.should != "" {
					h.Set("Stripe-Should-Retry", tc.should)
				}
				return testutil.StripeReply{Status: 429, Header: h, Body: `{"error":{"type":"rate_limit_error","message":"safe fixture"}}`}
			})
			opts := testutil.Options()
			opts.RetryBudget = tc.budget
			opts.CallTimeout = 100 * time.Millisecond
			j := journeyWith(t, db, db.Pool, s, opts)
			v, status := create(t, j, uuid.NewString())
			require.Equal(t, 202, status)
			id, _ := envelopeIDs(t, v)
			saved, e := j.Repo.LoadOrder(context.Background(), id)
			require.NoError(t, e)
			require.Equal(t, "unresolved", saved.Operation.State)
			wires := s.Wires()
			require.Len(t, wires, tc.calls)
			for n := 1; n < len(wires); n++ {
				minimum := tc.minimum
				if n == 2 && minimum < 500*time.Millisecond {
					minimum = 500 * time.Millisecond
				}
				require.GreaterOrEqual(t, wires[n].At.Sub(wires[n-1].At), minimum-20*time.Millisecond)
				require.Equal(t, wires[0].Header.Get("Idempotency-Key"), wires[n].Header.Get("Idempotency-Key"))
			}
		})
	}
	t.Run("cancel_during_real_wait", func(t *testing.T) {
		db := testutil.Database(t)
		s := testutil.NewStripeServer(t)
		s.SetScript(func(testutil.Wire) testutil.StripeReply {
			return testutil.StripeReply{Status: 429, Header: http.Header{"Retry-After": []string{"1"}}, Body: `{"error":{"type":"rate_limit_error","message":"fixture"}}`}
		})
		waiting := make(chan struct{})
		opts := testutil.Options()
		var once sync.Once
		opts.Wait = func(ctx context.Context, d time.Duration) error {
			once.Do(func() { close(waiting) })
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		j := journeyWith(t, db, db.Pool, s, opts)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		key := uuid.NewString()
		done := make(chan error, 1)
		go func() { _, e := j.Service.Create(ctx, createInput(key)); done <- e }()
		select {
		case <-waiting:
		case <-time.After(2 * time.Second):
			t.Fatal("retry wait not reached")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("retry wait did not join")
		}
		require.Len(t, s.Wires(), 1)
		b, e := j.Repo.LoadBinding(context.Background(), key)
		require.NoError(t, e)
		v, e := j.Repo.LoadOrder(context.Background(), b.OrderID)
		require.NoError(t, e)
		require.Equal(t, b.OperationID, v.Operation.ID)
		require.Equal(t, "unresolved", v.Operation.State)
	})
}

// FND-003 FND-004 STR-004
func TestActualCheckoutServerShutdown(t *testing.T) {
	for _, tc := range []struct {
		name, blocked string
		graceful      bool
	}{
		{"checkout_completes_during_grace", "stripe", true}, {"overdue_stripe_canceled", "stripe", false},
		{"overdue_database_canceled", "database", false}, {"overdue_body_canceled", "body", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			independent, status := create(t, j, uuid.NewString())
			require.Equal(t, 201, status)
			independentID, _ := envelopeIDs(t, independent)
			independentBefore, err := j.Repo.LoadOrder(context.Background(), independentID)
			require.NoError(t, err)
			env := testutil.Environment(j.DB.URL, "127.0.0.1:0")
			env["SHUTDOWN_GRACE"] = "150ms"
			env["CLEANUP_TIMEOUT"] = "1s"
			c, err := config.Load(func(k string) string { return env[k] })
			require.NoError(t, err)
			entered, joined, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var enterOnce, joinOnce sync.Once
			feature := web.NewCheckoutHandler(j.Service, j.Options.RequestTimeout, j.Logs.Logger())
			require.NotNil(t, feature)
			extra := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				enterOnce.Do(func() { close(entered) })
				defer joinOnce.Do(func() { close(joined) })
				feature.ServeHTTP(w, r)
			})
			server := web.New(c, j.Logs.Logger(), j.DB.Pool.Ping, extra)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			served := make(chan error, 1)
			go func() {
				served <- server.Serve(ctx, ln, func() error {
					select {
					case <-joined:
						close(cleaned)
						return nil
					default:
						return errors.New("cleanup before feature work joined")
					}
				})
			}()
			barrier := testutil.NewBarrier()
			defer barrier.Release()
			key := uuid.NewString()
			path := "/api/orders"
			body := purchaseBody(key, "Shutdown product", 2500)
			var expected payment.RequestBinding
			var release func()
			if tc.blocked == "stripe" {
				j.Stripe.Configure(func(s *testutil.StripeServer) {
					s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
				})
			}
			if tc.blocked == "database" {
				i := testutil.Intent()
				_, err = j.Repo.AcceptInitial(context.Background(), i)
				require.NoError(t, err)
				expected = i.Binding
				path = "/api/orders/" + i.Order.ID + "/checkout"
				body = `{"request_key":"` + key + `"}`
				release = testutil.HoldOrder(t, j.DB.Independent(t), i.Order.ID)
				defer release()
			}
			type httpResult struct {
				status int
				body   []byte
				err    error
			}
			response := make(chan httpResult, 1)
			if tc.blocked == "body" {
				conn, err := net.Dial("tcp", ln.Addr().String())
				require.NoError(t, err)
				defer conn.Close()
				auth := base64.StdEncoding.EncodeToString([]byte("checkout-fixture-user:checkout-fixture-password"))
				_, err = fmt.Fprintf(conn, "POST /api/orders HTTP/1.1\r\nHost: local\r\nAuthorization: Basic %s\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{", auth)
				require.NoError(t, err)
			} else {
				go func() {
					req, e := http.NewRequest("POST", "http://"+ln.Addr().String()+path, strings.NewReader(body))
					if e != nil {
						response <- httpResult{err: e}
						return
					}
					req.SetBasicAuth("checkout-fixture-user", "checkout-fixture-password")
					req.Header.Set("Content-Type", "application/json")
					client := &http.Client{Timeout: 4 * time.Second}
					res, e := client.Do(req)
					if e != nil {
						response <- httpResult{err: e}
						return
					}
					b, e := io.ReadAll(res.Body)
					closeErr := res.Body.Close()
					if e == nil {
						e = closeErr
					}
					response <- httpResult{res.StatusCode, b, e}
				}()
			}
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("checkout not admitted")
			}
			if tc.blocked == "stripe" {
				select {
				case <-barrier.Arrived:
				case <-time.After(2 * time.Second):
					t.Fatal("Stripe request not held")
				}
				expected, err = j.Repo.LoadBinding(context.Background(), key)
				require.NoError(t, err)
			}
			if tc.blocked == "database" {
				require.Eventually(t, func() bool {
					var n int
					e := j.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&n)
					return e == nil && n >= 1
				}, time.Second, 10*time.Millisecond)
			}
			before := j.DB.Durable(t)
			start := time.Now()
			stop()
			if tc.graceful {
				select {
				case <-cleaned:
					t.Fatal("cleanup overtook active checkout")
				default:
				}
				barrier.Release()
			}
			select {
			case e := <-served:
				require.NoError(t, e)
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown did not join checkout and cleanup")
			}
			require.Less(t, time.Since(start), 2*time.Second)
			select {
			case <-joined:
			default:
				t.Fatal("checkout owned work remained after cleanup")
			}
			select {
			case <-cleaned:
			default:
				t.Fatal("cleanup missing")
			}
			if tc.blocked == "stripe" {
				select {
				case <-barrier.Done:
				case <-time.After(time.Second):
					t.Fatal("SDK peer did not observe shutdown")
				}
			}
			if release != nil {
				release()
			}
			if tc.blocked != "body" {
				select {
				case result := <-response:
					if tc.graceful {
						require.NoError(t, result.err)
						require.Equal(t, 201, result.status)
					}
				case <-time.After(time.Second):
					t.Fatal("caller response not joined")
				}
			}
			if expected.OrderID != "" {
				view, err := j.Repo.LoadOrder(context.Background(), expected.OrderID)
				require.NoError(t, err)
				require.Equal(t, expected.OperationID, view.Operation.ID)
				require.Equal(t, "unpaid", view.Order.Status)
				if !tc.graceful {
					require.NotEqual(t, "rejected", view.Operation.State)
					require.Nil(t, view.Operation.SessionID)
				}
			} else {
				require.Equal(t, before, j.DB.Durable(t), "stalled body cannot accept work")
			}
			after, err := j.Repo.LoadOrder(context.Background(), independentID)
			require.NoError(t, err)
			require.Equal(t, independentBefore, after, "independent committed success survives shutdown")
			snapshot := j.DB.Durable(t)
			wireCount := len(j.Stripe.Wires())
			spy := &readSpy{Reader: strings.NewReader("{broken")}
			r := testutil.Request("POST", "/api/orders", "", true)
			r.Body = spy
			w := testutil.Response(t, server.Handler, r)
			require.Equal(t, 503, w.Code)
			require.Zero(t, spy.reads, "shutdown admission precedes protected body parsing")
			require.Equal(t, snapshot, j.DB.Durable(t))
			require.Equal(t, wireCount, len(j.Stripe.Wires()))
		})
	}
}

// STR-004 STR-005 STR-006 LIFE-009
func TestMixedElapsedBudgetCapsCallsAndRetainsPreparedIntent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		getDelay time.Duration
		post     bool
	}{{"remaining_call_cap", 200 * time.Millisecond, true}, {"elapsed_exhausted_after_get", 200 * time.Millisecond, false}} {
		t.Run(tc.name, func(t *testing.T) {
			j := journey(t)
			v, status := create(t, j, uuid.NewString())
			require.Equal(t, 201, status)
			id, _ := envelopeIDs(t, v)
			peer := testutil.NewBarrier()
			defer peer.Release()
			j.Stripe.Configure(func(s *testutil.StripeServer) {
				s.Before = func(ctx context.Context, w testutil.Wire) {
					if w.Method == "GET" {
						timer := time.NewTimer(tc.getDelay)
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-ctx.Done():
						}
					} else {
						_ = peer.Wait(ctx)
					}
				}
			})
			j.Stripe.SetScript(func(w testutil.Wire) testutil.StripeReply {
				if w.Method == "GET" {
					return testutil.StripeReply{Mutation: func(o map[string]any) { o["status"] = "expired"; o["payment_status"] = "unpaid"; o["url"] = "" }}
				}
				return testutil.StripeReply{}
			})
			opts := j.Options
			opts.RetryBudget = 500 * time.Millisecond
			opts.CallTimeout = 500 * time.Millisecond
			opts.RequestTimeout = 2 * time.Second
			current := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, opts)
			key := uuid.NewString()
			var held *committedPreparationHold
			if !tc.post {
				held = &committedPreparationHold{Repository: current.Repo, until: func() time.Time {
					wires := j.Stripe.Wires()
					// The shared external budget starts no later than the first GET wire.
					// Holding only the already-committed return preserves final-work time.
					return wires[len(wires)-1].At.Add(opts.RetryBudget + 20*time.Millisecond)
				}}
				gateway := stripeapi.New("sk_test_fixture", stripeapi.Options{BackendURL: j.Stripe.Server.URL, HTTPClient: j.Stripe.Server.Client()})
				service := payment.New(held, gateway, current.Options)
				env := testutil.Environment(j.DB.URL, "localhost:8080")
				env["CHECKOUT_REQUEST_TIMEOUT"] = opts.RequestTimeout.String()
				env["STRIPE_CALL_TIMEOUT"] = opts.CallTimeout.String()
				env["STRIPE_RETRY_BUDGET"] = opts.RetryBudget.String()
				env["STRIPE_MAX_ATTEMPTS"] = fmt.Sprint(opts.MaxAttempts)
				c, err := config.Load(func(k string) string { return env[k] })
				require.NoError(t, err)
				require.NoError(t, c.ValidateServing())
				current.Handler = web.New(c, current.Logs.Logger(), j.DB.Pool.Ping, web.NewCheckoutHandler(service, opts.RequestTimeout, current.Logs.Logger())).Handler
			}
			before := len(j.Stripe.Wires())
			start := time.Now()
			request := testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true)
			requestCtx, cancel := context.WithTimeout(request.Context(), opts.RequestTimeout)
			defer cancel()
			if held != nil {
				held.requestCtx = requestCtx
			}
			w := testutil.Response(t, current.Handler, request.WithContext(requestCtx))
			require.Equal(t, 202, w.Code)
			require.Less(t, time.Since(start), time.Second)
			result := testutil.JSON(t, w)
			_, op := envelopeIDs(t, result)
			wires := j.Stripe.Wires()[before:]
			if tc.post {
				require.Len(t, wires, 2)
				require.Equal(t, "POST", wires[1].Method)
				require.Less(t, time.Since(wires[1].At), 300*time.Millisecond, "last call uses remaining elapsed budget, not configured full timeout")
				select {
				case <-peer.Done:
				case <-time.After(time.Second):
					t.Fatal("capped SDK call not joined")
				}
			} else {
				require.Len(t, wires, 1)
				require.Equal(t, "GET", wires[0].Method)
				require.Equal(t, op, held.committed.Operation.ID)
				require.Equal(t, "prepared", held.committed.Operation.State)
				require.True(t, held.releasedAt.After(wires[0].At.Add(opts.RetryBudget)), "external elapsed budget is genuinely exhausted before prepared work returns")
				require.GreaterOrEqual(t, time.Since(start), opts.RetryBudget)
				require.Equal(t, "prepared", result["operation"].(map[string]any)["state"])
			}
			binding, err := current.Repo.LoadBinding(context.Background(), key)
			require.NoError(t, err)
			require.Equal(t, op, binding.OperationID)
			j.Stripe.Configure(func(s *testutil.StripeServer) { s.Before = nil })
			j.Stripe.SetScript(nil)
			fresh := journeyWith(t, j.DB, j.DB.Independent(t), j.Stripe, j.Options)
			w = testutil.Response(t, fresh.Handler, testutil.Request("POST", "/api/orders/"+id+"/checkout", `{"request_key":"`+key+`"}`, true))
			require.Equal(t, 200, w.Code)
			_, recovered := envelopeIDs(t, testutil.JSON(t, w))
			require.Equal(t, op, recovered)
			require.Equal(t, 2, j.DB.Counts(t)["payment_operations"])
		})
	}
}

// STR-002 STR-003 STR-007
func TestRealRetriesExposeDurableLastDispatchBeforeEachSend(t *testing.T) {
	j := journey(t)
	control := j.DB.Independent(t)
	type observed struct {
		at          time.Time
		first, last time.Time
		err         error
	}
	observations := make(chan observed, 3)
	j.Stripe.Configure(func(s *testutil.StripeServer) {
		s.Status = 500
		s.ErrorBody = `{"error":{"type":"api_error","message":"fixture"}}`
		s.Before = func(ctx context.Context, w testutil.Wire) {
			var o observed
			o.at = w.At
			var orderID string
			o.err = control.QueryRow(ctx, `SELECT order_id FROM payment_operations WHERE stripe_key=$1`, w.Header.Get("Idempotency-Key")).Scan(&orderID)
			if o.err == nil {
				independent := postgres.NewPaymentRepository(control)
				view, e := independent.LoadOrder(ctx, orderID)
				o.err = e
				if e == nil && view.Operation.FirstDispatchAt != nil && view.Operation.LastDispatchAt != nil {
					o.first = *view.Operation.FirstDispatchAt
					o.last = *view.Operation.LastDispatchAt
				}
			}

			observations <- o
		}
	})
	v, status := create(t, j, uuid.NewString())
	require.Equal(t, 202, status)
	id, op := envelopeIDs(t, v)
	saved, err := j.Repo.LoadOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, op, saved.Operation.ID)
	var previous observed
	for n := range 3 {
		select {
		case o := <-observations:
			require.NoError(t, o.err)
			require.False(t, o.last.After(o.at), "possible-send marker visible before SDK peer arrival")
			require.Less(t, o.at.Sub(o.last), time.Second)
			if n > 0 {
				require.Equal(t, previous.first, o.first)
				require.True(t, o.last.After(previous.last))
			}
			previous = o
		case <-time.After(time.Second):
			t.Fatal("independent before-send DB evidence missing")
		}
	}
	require.Equal(t, *saved.Operation.LastDispatchAt, previous.last)
	require.Len(t, j.Stripe.Wires(), 3)
}

// committedPreparationHold delays only the return of a successful, committed
// eligibility/preparation transaction. No DB transaction is held while waiting.
type committedPreparationHold struct {
	payment.Repository
	until      func() time.Time
	requestCtx context.Context
	committed  payment.View
	releasedAt time.Time
}

func (r *committedPreparationHold) BindContinuation(ctx context.Context, intent payment.ContinuationIntent) (payment.View, error) {
	v, err := r.Repository.BindContinuation(ctx, intent)
	if err != nil {
		return v, err
	}
	r.committed = v
	timer := time.NewTimer(time.Until(r.until()))
	defer timer.Stop()
	select {
	case <-timer.C:
		r.releasedAt = time.Now()
		return v, nil
	case <-r.requestCtx.Done():
		return v, r.requestCtx.Err()
	}
}
