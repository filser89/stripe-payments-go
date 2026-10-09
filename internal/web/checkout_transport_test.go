package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const checkoutAuthorization = "Basic Y2hlY2tvdXQtZml4dHVyZS11c2VyOmNoZWNrb3V0LWZpeHR1cmUtcGFzc3dvcmQ="

type observedCheckoutBody struct {
	io.ReadCloser
	bytes atomic.Int64
}

func (b *observedCheckoutBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes.Add(int64(n))
	return n, err
}

type checkoutTransportObservation struct {
	bytes   int64
	elapsed time.Duration
}

func realCheckoutTransport(t *testing.T, f *checkoutFake, parent time.Duration) (string, <-chan checkoutTransportObservation) {
	t.Helper()
	env := testutil.Environment("postgres://fixture:fixture@localhost/fixture", "127.0.0.1:8080")
	env["CHECKOUT_REQUEST_TIMEOUT"] = "2s"
	env["STRIPE_CALL_TIMEOUT"] = "100ms"
	env["STRIPE_RETRY_BUDGET"] = "500ms"
	env["STRIPE_MAX_ATTEMPTS"] = "1"
	env["HTTP_READ_TIMEOUT"] = "3s"
	env["HTTP_WRITE_TIMEOUT"] = "3s"
	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NoError(t, c.ValidateServing())
	require.Equal(t, 2*time.Second, c.CheckoutSettings().RequestTimeout)
	logs := &testutil.Logs{}
	extra := NewCheckoutHandler(f, c.CheckoutSettings().RequestTimeout, logs.Logger())
	require.NotNil(t, extra)
	completed := make(chan checkoutTransportObservation, 1)
	observing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		body := &observedCheckoutBody{ReadCloser: r.Body}
		r.Body = body
		if parent > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), parent)
			defer cancel()
			r = r.WithContext(ctx)
		}
		extra.ServeHTTP(w, r)
		completed <- checkoutTransportObservation{body.bytes.Load(), time.Since(start)}
	})
	s := New(c, logs.Logger(), func(context.Context) error { return nil }, observing)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln, func() error { return nil }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(4 * time.Second):
			t.Error("serving work did not join shutdown")
		}
	})
	return ln.Addr().String(), completed
}
func checkoutTCP(t *testing.T, address, method, path, framing, body string) (*http.Response, error) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\nContent-Type: application/json\r\nConnection: close\r\n%s\r\n%s", method, path, checkoutAuthorization, framing, body)
	require.NoError(t, err)
	return http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
}
func checkoutTransportJoined(t *testing.T, completed <-chan checkoutTransportObservation, bound time.Duration) checkoutTransportObservation {
	t.Helper()
	select {
	case observed := <-completed:
		require.Less(t, observed.elapsed, bound, "service-side completion must fit admission deadline")
		return observed
	case <-time.After(bound + time.Second):
		t.Fatal("request handler work did not complete")
		return checkoutTransportObservation{}
	}
}
func TestCheckoutRealBodyAndResponseDeadlines(t *testing.T) { // INP-005 INP-006 STR-004 FND-003
	t.Run("stalled_chunked_body", func(t *testing.T) {
		f := preparedCheckout()
		address, completed := realCheckoutTransport(t, f, 0)
		start := time.Now()
		response, err := checkoutTCP(t, address, "POST", "/api/orders", "Transfer-Encoding: chunked\r\n", "1\r\n{\r\n")
		assertCheckoutTransportRejection(t, response, err, "POST")
		observed := checkoutTransportJoined(t, completed, 2500*time.Millisecond)
		require.GreaterOrEqual(t, time.Since(start), 1500*time.Millisecond)
		require.LessOrEqual(t, observed.bytes, int64(4097))
		require.Zero(t, f.Effects())
	})
	t.Run("operation_parent_cancellation_join", func(t *testing.T) {
		f := preparedCheckout()
		barrier := testutil.NewBarrier()
		f.Before = func(ctx context.Context) { _ = barrier.Wait(ctx) }
		h, _ := checkoutHandler(t, f, 2*time.Second)
		r := testutil.Request("POST", "/api/orders", `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`, true)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		done := make(chan struct{})
		go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
		select {
		case <-barrier.Arrived:
		case <-time.After(time.Second):
			t.Fatal("operation not admitted")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("handler did not join")
		}
		select {
		case <-barrier.Done:
		case <-time.After(time.Second):
			t.Fatal("owned operation survived response")
		}
	})
}
func assertCheckoutTransportRejection(t *testing.T, response *http.Response, err error, method string) {
	t.Helper()
	if err != nil {
		var timeout net.Error
		require.False(t, errors.As(err, &timeout) && timeout.Timeout(), "client deadline cannot establish server completion")
		return
	}
	defer func() { assert.NoError(t, response.Body.Close()) }()
	require.Equal(t, 400, response.StatusCode)
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	if method == "HEAD" {
		require.Empty(t, data)
	} else {
		var v map[string]any
		require.NoError(t, json.Unmarshal(data, &v))
		e, ok := v["error"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "invalid_request", e["code"])
		require.NotEmpty(t, e["message"])
		require.NotContains(t, string(data), "sensitive-sentinel")
	}
}
func TestCheckoutTCPByteBoundaries(t *testing.T) { // INP-005 HTTP-004 STR-004
	for _, tc := range []struct {
		name                  string
		continuation, chunked bool
		size, status          int
	}{
		{"initial_length_4096", false, false, 4096, 201}, {"initial_length_4097", false, false, 4097, 413}, {"initial_chunked_4096", false, true, 4096, 201}, {"initial_chunked_4097", false, true, 4097, 413},
		{"checkout_length_4096", true, false, 4096, 201}, {"checkout_length_4097", true, false, 4097, 413}, {"checkout_chunked_4096", true, true, 4096, 201}, {"checkout_chunked_4097", true, true, 4097, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			address, completed := realCheckoutTransport(t, f, 0)
			path := "/api/orders"
			body := `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`
			if tc.continuation {
				path += "/" + f.Outcome.Order.ID + "/checkout"
				body = `{"request_key":"11111111-1111-4111-8111-111111111111"}`
			}
			body += strings.Repeat(" ", tc.size-len(body))
			framing := fmt.Sprintf("Content-Length: %d\r\n", tc.size)
			if tc.chunked {
				framing = "Transfer-Encoding: chunked\r\n"
				body = fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", len(body), body)
			}
			response, err := checkoutTCP(t, address, "POST", path, framing, body)
			require.NoError(t, err)
			defer func() { assert.NoError(t, response.Body.Close()) }()
			require.Equal(t, tc.status, response.StatusCode)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
			require.NotEmpty(t, response.Header.Get("X-Request-ID"))
			observed := checkoutTransportJoined(t, completed, 2500*time.Millisecond)
			require.LessOrEqual(t, observed.bytes, int64(4097))
			if tc.status >= 400 {
				require.Zero(t, f.Effects())
				var v map[string]any
				require.NoError(t, json.Unmarshal(data, &v))
				require.Equal(t, "body_too_large", v["error"].(map[string]any)["code"])
			} else {
				require.EqualValues(t, 1, f.Effects())
				require.Equal(t, int64(4096), observed.bytes)
			}
		})
	}
}
func TestCheckoutTCPStalledReads(t *testing.T) { // INP-005 INP-006 STR-004
	for _, tc := range []struct {
		name, method, suffix string
		chunked, earlier     bool
	}{
		{"initial_content_length", "POST", "", false, false}, {"checkout_content_length", "POST", "/11111111-1111-4111-8111-111111111111/checkout", false, false},
		{"get_chunked", "GET", "/11111111-1111-4111-8111-111111111111", true, false}, {"head_chunked", "HEAD", "/11111111-1111-4111-8111-111111111111", true, false},
		{"get_content_length", "GET", "/11111111-1111-4111-8111-111111111111", false, false}, {"head_content_length", "HEAD", "/11111111-1111-4111-8111-111111111111", false, false},
		{"initial_earlier_parent", "POST", "", false, true}, {"checkout_earlier_parent", "POST", "/11111111-1111-4111-8111-111111111111/checkout", true, true}, {"get_earlier_parent", "GET", "/11111111-1111-4111-8111-111111111111", true, true}, {"head_earlier_parent", "HEAD", "/11111111-1111-4111-8111-111111111111", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			parent := time.Duration(0)
			bound := 2500 * time.Millisecond
			if tc.earlier {
				parent = 100 * time.Millisecond
				bound = time.Second
			}
			address, completed := realCheckoutTransport(t, f, parent)
			framing := "Content-Length: 1\r\n"
			if tc.chunked {
				framing = "Transfer-Encoding: chunked\r\n"
			}
			start := time.Now()
			response, err := checkoutTCP(t, address, tc.method, "/api/orders"+tc.suffix, framing, "")
			assertCheckoutTransportRejection(t, response, err, tc.method)
			observed := checkoutTransportJoined(t, completed, bound)
			require.Less(t, time.Since(start), bound, "final transport completion must honor effective admission deadline")
			require.Zero(t, observed.bytes)
			require.Zero(t, f.Effects())
		})
	}
}
func TestCheckoutTCPUnknownLengthReads(t *testing.T) { // INP-006 HTTP-004
	for _, tc := range []struct {
		name, method, chunks string
		status               int
	}{
		{"get_empty", "GET", "0\r\n\r\n", 200}, {"head_empty", "HEAD", "0\r\n\r\n", 200}, {"get_nonempty", "GET", "1\r\nx\r\n0\r\n\r\n", 400}, {"head_nonempty", "HEAD", "1\r\nx\r\n0\r\n\r\n", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			address, completed := realCheckoutTransport(t, f, 0)
			response, err := checkoutTCP(t, address, tc.method, "/api/orders/"+f.View.Order.ID, "Transfer-Encoding: chunked\r\n", tc.chunks)
			require.NoError(t, err)
			defer func() { assert.NoError(t, response.Body.Close()) }()
			require.Equal(t, tc.status, response.StatusCode)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			if tc.method == "HEAD" {
				require.Empty(t, data)
			}
			observed := checkoutTransportJoined(t, completed, 2500*time.Millisecond)
			require.LessOrEqual(t, observed.bytes, int64(1))
			if tc.status == 400 {
				require.Zero(t, f.Effects())
			} else {
				require.EqualValues(t, 1, f.Read.Load())
			}
		})
	}
}

type checkoutPipeListener struct {
	conn     net.Conn
	accepted bool
	closed   chan struct{}
	once     sync.Once
}

func (l *checkoutPipeListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *checkoutPipeListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*checkoutPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}

type checkoutObservedWrite struct {
	net.Conn
	done chan error
}

func (c *checkoutObservedWrite) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	select {
	case c.done <- err:
	default:
	}
	return n, err
}
func TestCheckoutBlockedResponseWriteCompletion(t *testing.T) { // STR-004 FND-003
	env := testutil.Environment("postgres://fixture:fixture@localhost/fixture", "localhost:8080")
	env["CHECKOUT_REQUEST_TIMEOUT"] = "2s"
	env["STRIPE_CALL_TIMEOUT"] = "100ms"
	env["STRIPE_RETRY_BUDGET"] = "500ms"
	env["HTTP_READ_TIMEOUT"] = "3s"
	env["HTTP_WRITE_TIMEOUT"] = "3s"
	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NoError(t, c.ValidateServing())
	require.Equal(t, 2*time.Second, c.CheckoutSettings().RequestTimeout)
	f := preparedCheckout()
	logs := &testutil.Logs{}
	extra := NewCheckoutHandler(f, c.CheckoutSettings().RequestTimeout, logs.Logger())
	require.NotNil(t, extra)
	joined := make(chan struct{})
	s := New(c, logs.Logger(), func(context.Context) error { return nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { extra.ServeHTTP(w, r); close(joined) }))
	serverSide, clientSide := net.Pipe()
	defer func() { _ = clientSide.Close() }()
	writes := make(chan error, 1)
	observed := &checkoutObservedWrite{serverSide, writes}
	ln := &checkoutPipeListener{conn: observed, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln, func() error { return nil }) }()
	t.Cleanup(func() {
		cancel()
		_ = clientSide.Close()
		select {
		case err := <-served:
			assert.NoError(t, err)
		case <-time.After(4 * time.Second):
			t.Error("HTTP response worker did not join shutdown")
		}
	})
	body := `{"description":"single product","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`
	require.NoError(t, clientSide.SetWriteDeadline(time.Now().Add(time.Second)))
	start := time.Now()
	_, err = fmt.Fprintf(clientSide, "POST /api/orders HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", checkoutAuthorization, len(body), body)
	require.NoError(t, err)
	// net.Pipe has no send buffer: leaving the client unread blocks the real
	// response write until the effective admission deadline releases it.
	select {
	case err := <-writes:
		var timeout net.Error
		require.ErrorAs(t, err, &timeout)
		require.True(t, timeout.Timeout())
		require.Less(t, time.Since(start), 2500*time.Millisecond)
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("response write outlived admitted request budget")
	}
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("handler work remained live after response completion")
	}
	require.EqualValues(t, 1, f.Created.Load())
	require.Zero(t, f.Continued.Load())
}
