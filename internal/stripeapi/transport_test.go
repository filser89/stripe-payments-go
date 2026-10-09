package stripeapi

import (
	"context"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSDKStructuredAndUncertainErrors(t *testing.T) { // STR-005 STR-006 STR-007 STR-008
	for _, tc := range []struct {
		name        string
		status      int
		body, class string
	}{
		{"validation", 400, `{"error":{"type":"invalid_request_error","param":"line_items","message":"sensitive-sentinel"}}`, "validation"},
		{"authentication", 401, `{"error":{"type":"authentication_error","message":"sensitive-sentinel"}}`, "credential"},
		{"permission", 403, `{"error":{"type":"permission_error","message":"sensitive-sentinel"}}`, "permission"},
		{"rate_limit", 429, `{"error":{"type":"rate_limit_error","message":"sensitive-sentinel"}}`, "rate_limit"},
		{"idempotency", 400, `{"error":{"type":"idempotency_error","message":"sensitive-sentinel"}}`, "idempotency"},
		{"transient_conflict", 409, `{"error":{"type":"invalid_request_error","code":"idempotency_key_in_use","message":"sensitive-sentinel"}}`, "transient_conflict"},
		{"server", 500, `{"error":{"type":"api_error","message":"sensitive-sentinel"}}`, "server"},
		{"generic", 400, `{"error":{"message":"sensitive-sentinel"}}`, "generic"},
		{"malformed", 200, `{broken`, "malformed"},
		{"unstructured", 502, `sensitive-sentinel`, "server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewStripeServer(t)
			server.Status = tc.status
			server.ErrorBody = tc.body
			g := New("sk_test_fixture", Options{BackendURL: server.Server.URL, HTTPClient: server.Server.Client()})
			require.NotNil(t, g, "missing SDK error classification")
			e, err := g.Create(context.Background(), testutil.Intent().Operation.Snapshot)
			require.Error(t, err)
			require.Equal(t, tc.class, e.ErrorClass)
			require.NotContains(t, err.Error(), "sensitive-sentinel")
			require.Len(t, server.Wires(), 1, "adapter must make one attempt; domain owns retries")
		})
	}
}
func TestSTR004SDKTransport(t *testing.T) { // STR-004 STR-005
	t.Run("blocked_headers_deadline", func(t *testing.T) {
		s := testutil.NewStripeServer(t)
		barrier := testutil.NewBarrier()
		s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		g := New("sk_test_fixture", Options{BackendURL: s.Server.URL, HTTPClient: s.Server.Client()})
		require.NotNil(t, g, "missing cancellable SDK adapter")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := g.Create(ctx, testutil.Intent().Operation.Snapshot)
		require.Error(t, err)
		require.True(t, errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil)
		require.Less(t, time.Since(start), time.Second)
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("SDK wire work remained blocked")
		}
		require.Len(t, s.Wires(), 1)
	})
	t.Run("earlier_parent_cancellation", func(t *testing.T) {
		s := testutil.NewStripeServer(t)
		barrier := testutil.NewBarrier()
		s.Before = func(ctx context.Context, _ testutil.Wire) { _ = barrier.Wait(ctx) }
		g := New("sk_test_fixture", Options{BackendURL: s.Server.URL, HTTPClient: s.Server.Client()})
		require.NotNil(t, g, "missing SDK parent cancellation")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := g.Create(ctx, testutil.Intent().Operation.Snapshot); done <- err }()
		select {
		case <-barrier.Arrived:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("no wire")
		}
		cancel()
		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(time.Second):
			t.Fatal("SDK did not join")
		}
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("server did not join")
		}
	})
	t.Run("reset_no_hidden_retry", func(t *testing.T) {
		s := testutil.NewStripeServer(t)
		s.Drop = true
		g := New("sk_test_fixture", Options{BackendURL: s.Server.URL, HTTPClient: s.Server.Client()})
		require.NotNil(t, g, "missing reset handling")
		_, err := g.Create(context.Background(), testutil.Intent().Operation.Snapshot)
		require.Error(t, err)
		require.Len(t, s.Wires(), 1)
		require.Equal(t, 1, s.LogicalObjects())
	})
}
