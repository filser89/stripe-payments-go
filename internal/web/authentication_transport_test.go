package web_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transportSettings(t *testing.T) config.Config {
	t.Helper()
	c := authSettings(t, authUser, authPassword)
	c.HeaderTimeout = 100 * time.Millisecond
	c.ReadTimeout = 150 * time.Millisecond
	c.WriteTimeout = 500 * time.Millisecond
	c.IdleTimeout = 100 * time.Millisecond
	c.ShutdownGrace = 100 * time.Millisecond
	c.CleanupTimeout = time.Second
	return c
}

func startTransport(t *testing.T, c config.Config, extra http.Handler) string {
	t.Helper()
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, extra)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveTransport(t, s, ln)
	return ln.Addr().String()
}

func serveTransport(t *testing.T, s *web.Server, ln net.Listener) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln, func() error { return nil }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Error("HTTP server failed to stop")
		}
	})
}

func transportConn(t *testing.T, address string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, c.Close()) })
	require.NoError(t, c.SetDeadline(time.Now().Add(2*time.Second)))
	return c
}

func TestAuthenticationRealStreamingBodies(t *testing.T) { // HTTP-005 V1–V3
	address := startTransport(t, transportSettings(t), nil)
	for _, tc := range []struct {
		name, chunks string
		status       int
	}{
		{"empty_chunked", "0\r\n\r\n", http.StatusOK},
		{"nonempty_chunked", "1\r\nx\r\n0\r\n\r\n", http.StatusBadRequest},
	} {
		c := transportConn(t, address)
		_, err := fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n%s", authHeader(authUser, authPassword), tc.chunks)
		require.NoError(t, err)
		response, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: http.MethodGet})
		require.NoError(t, err, tc.name)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, tc.status, response.StatusCode, tc.name)
		if tc.status == http.StatusOK {
			assert.Contains(t, response.Header.Get("Content-Type"), "text/html")
			assert.Contains(t, strings.ToLower(string(data)), "payment")
		} else {
			assert.NotEmpty(t, strings.TrimSpace(string(data)))
			assert.NotContains(t, string(data), authPassword)
		}
	}
}

func TestAuthenticationStalledBodyDeadline(t *testing.T) { // HTTP-006 V1–V3, HTTP-004 V4
	cfg := transportSettings(t)
	address := startTransport(t, cfg, nil)
	c := transportConn(t, address)
	start := time.Now()
	_, err := fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\nTransfer-Encoding: chunked\r\n\r\n", authHeader(authUser, authPassword))
	require.NoError(t, err)
	// No chunk or terminating EOF is sent. A body-free handler would falsely
	// succeed; a handler without the server read deadline would remain blocked.
	response, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: http.MethodGet})
	assert.GreaterOrEqual(t, time.Since(start), cfg.ReadTimeout/2)
	assert.Less(t, time.Since(start), time.Second)
	if err != nil {
		var timeout net.Error
		assert.False(t, errors.As(err, &timeout) && timeout.Timeout(), "client deadline cannot stand in for a server deadline")
		return
	}
	defer func() { assert.NoError(t, response.Body.Close()) }()
	assert.NotEqual(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	data, readErr := io.ReadAll(response.Body)
	assert.NoError(t, readErr)
	assert.NotEmpty(t, strings.TrimSpace(string(data)))
	assert.NotContains(t, string(data), authPassword)
}

// pipeListener makes blocked network writes deterministic: net.Pipe has no
// socket send buffer that could absorb the response while its peer does not read.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }

func TestAuthenticationFoundationTransport(t *testing.T) { // FND-004 V1–V5
	cfg := transportSettings(t)
	address := startTransport(t, cfg, nil)
	// V1: incomplete headers never reach routing or authentication.
	{
		c := transportConn(t, address)
		_, err := io.WriteString(c, "GET /healthz HTTP/1.1\r\nHost: localhost\r\nX-Incomplete:")
		require.NoError(t, err)
		start := time.Now()
		_, err = bufio.NewReader(c).ReadByte()
		assert.Error(t, err)
		assert.GreaterOrEqual(t, time.Since(start), cfg.HeaderTimeout/2)
		assert.Less(t, time.Since(start), time.Second)
	}
	// V2: the transport enforces its read deadline independently of the landing
	// route. HTTP-006 additionally checks the actual landing stream.
	{
		readDone := make(chan error, 1)
		workAddress := startTransport(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.Copy(io.Discard, r.Body)
			readDone <- err
			w.WriteHeader(http.StatusBadRequest)
		}))
		c := transportConn(t, workAddress)
		_, err := fmt.Fprintf(c, "POST /work HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\nContent-Length: 1\r\n\r\n", authHeader(authUser, authPassword))
		require.NoError(t, err)
		start := time.Now()
		select {
		case err := <-readDone:
			var timeout net.Error
			assert.ErrorAs(t, err, &timeout)
			if timeout != nil {
				assert.True(t, timeout.Timeout())
			}
			assert.GreaterOrEqual(t, time.Since(start), cfg.ReadTimeout/2)
			assert.Less(t, time.Since(start), time.Second)
		case <-time.After(time.Second):
			t.Error("server read deadline did not release the body read")
		}
	}
	// V3: exercise the actual http.Server write deadline over a controlled
	// connection, rather than merely asserting timeout configuration fields.
	{
		writeDone := make(chan error, 1)
		writeCfg := cfg
		writeCfg.WriteTimeout = 100 * time.Millisecond
		s := web.New(writeCfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, err := io.Copy(w, strings.NewReader(strings.Repeat("x", 1<<20)))
			writeDone <- err
		}))
		serverSide, clientSide := net.Pipe()
		l := &pipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{})}
		l.connections <- serverSide
		serveTransport(t, s, l)
		t.Cleanup(func() { assert.NoError(t, clientSide.Close()) })
		require.NoError(t, clientSide.SetWriteDeadline(time.Now().Add(time.Second)))
		_, err := fmt.Fprintf(clientSide, "GET /work HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s\r\n\r\n", authHeader(authUser, authPassword))
		require.NoError(t, err)
		start := time.Now()
		select {
		case err := <-writeDone:
			var timeout net.Error
			assert.ErrorAs(t, err, &timeout)
			if timeout != nil {
				assert.True(t, timeout.Timeout())
			}
			assert.Less(t, time.Since(start), time.Second)
		case <-time.After(time.Second):
			t.Error("server write deadline did not release the blocked write")
		}
	}
	// V4: successful keepalive requests expire while the client is idle.
	{
		c := transportConn(t, address)
		_, err := io.WriteString(c, "GET /healthz HTTP/1.1\r\nHost: localhost\r\n\r\n")
		require.NoError(t, err)
		reader := bufio.NewReader(c)
		response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, response.StatusCode)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		start := time.Now()
		_, err = reader.ReadByte()
		assert.ErrorIs(t, err, io.EOF)
		assert.Less(t, time.Since(start), time.Second)
	}
	// V5: malformed/oversized transport headers follow net/http's contracts;
	// neither requires an application Basic challenge to be delivered.
	for _, tc := range []struct {
		request string
		status  int
	}{
		{"GET /healthz HTTP/1.1\r\nHost: localhost\r\nInvalid Header: x\r\n\r\n", http.StatusBadRequest},
		{"GET /healthz HTTP/1.1\r\nHost: localhost\r\nX-Large: " + strings.Repeat("x", 2<<20) + "\r\n\r\n", http.StatusRequestHeaderFieldsTooLarge},
	} {
		c := transportConn(t, address)
		_, writeErr := io.WriteString(c, tc.request)
		response, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: http.MethodGet})
		if err != nil {
			// Closing a malformed connection is a valid transport rejection; a
			// client-side deadline is not evidence of a bounded server response.
			var timeout net.Error
			assert.False(t, errors.As(err, &timeout) && timeout.Timeout())
			assert.Error(t, writeErr)
			continue
		}
		assert.Equal(t, tc.status, response.StatusCode)
		assert.Empty(t, response.Header.Get("WWW-Authenticate"))
		require.NoError(t, response.Body.Close())
	}
}
