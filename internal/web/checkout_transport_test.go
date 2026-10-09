package web

import (
	"bufio"
	"context"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckoutRealBodyAndResponseDeadlines(t *testing.T) { // INP-005 INP-006 STR-004 FND-003
	t.Run("stalled_chunked_body", func(t *testing.T) {
		f := preparedCheckout()
		h, _ := checkoutHandler(t, f, 100*time.Millisecond)
		server := httptest.NewServer(h)
		defer server.Close()
		conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
		_, err = fmt.Fprintf(conn, "POST /api/orders HTTP/1.1\r\nHost: localhost\r\nAuthorization: Basic Y2hlY2tvdXQtZml4dHVyZS11c2VyOmNoZWNrb3V0LWZpeHR1cmUtcGFzc3dvcmQ=\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n1\r\n{\r\n")
		require.NoError(t, err)
		start := time.Now()
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err == nil {
			_ = response.Body.Close()
			require.GreaterOrEqual(t, response.StatusCode, 400)
		}
		require.Less(t, time.Since(start), time.Second, "context alone cannot leave read blocked")
		require.Zero(t, f.Effects())
	})
	t.Run("operation_parent_cancellation_join", func(t *testing.T) {
		f := preparedCheckout()
		barrier := testutil.NewBarrier()
		f.Before = func(ctx context.Context) { _ = barrier.Wait(ctx) }
		h, _ := checkoutHandler(t, f, time.Second)
		r := testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		done := make(chan struct{})
		go func() { _ = testutil.Response(t, h, r); close(done) }()
		select {
		case <-barrier.Arrived:
		case <-time.After(time.Second):
			t.Fatal("operation not admitted")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("handler did not join canceled operation")
		}
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("owned operation survived response")
		}
	})
}
