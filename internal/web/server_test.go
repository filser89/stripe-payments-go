package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/web"
	"github.com/stretchr/testify/require"
)

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *safeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func settings(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(func(k string) string {
		if k == "DATABASE_URL" {
			return "postgres://app:SECRET@localhost/app"
		}
		if k == "BASIC_AUTH_USERNAME" {
			return "shutdown-fixture-user"
		}
		if k == "BASIC_AUTH_PASSWORD" {
			return "shutdown-fixture-password"
		}
		return ""
	})
	require.NoError(t, err)
	return c
}
func TestHealthReadinessAndSanitizedLogs(t *testing.T) { // FND-001
	var logs safeBuffer
	var calls atomic.Int32
	var unavailable atomic.Bool
	s := web.New(settings(t), slog.New(slog.NewJSONHandler(&logs, nil)), func(context.Context) error {
		calls.Add(1)
		if unavailable.Load() {
			return errors.New("postgres://app:SECRET@host/db")
		}
		return nil
	}, nil)
	health := httptest.NewRecorder()
	s.Handler.ServeHTTP(health, httptest.NewRequest("GET", "/healthz?token=SECRET", nil))
	require.Equal(t, 200, health.Code)
	require.EqualValues(t, 0, calls.Load())
	ready := httptest.NewRecorder()
	s.Handler.ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, 200, ready.Code)
	require.EqualValues(t, 1, calls.Load())
	unavailable.Store(true)
	failed := httptest.NewRecorder()
	s.Handler.ServeHTTP(failed, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, 503, failed.Code)
	require.NotContains(t, failed.Body.String(), "SECRET")
	unavailable.Store(false)
	recovered := httptest.NewRecorder()
	s.Handler.ServeHTTP(recovered, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, 200, recovered.Code)
	id := health.Header().Get("X-Request-ID")
	require.NotEmpty(t, id)
	require.NotContains(t, logs.String(), "SECRET")
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.Split(strings.TrimSpace(logs.String()), "\n")[0]), &entry))
	require.Equal(t, id, entry["request_id"])
	require.EqualValues(t, 200, entry["status"])
	require.Contains(t, entry, "duration_ms")
	require.Contains(t, logs.String(), "database_unavailable")
}
func TestReadinessDeadline(t *testing.T) { // FND-001
	c := settings(t)
	c.ReadinessTimeout = 20 * time.Millisecond
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, nil)
	start := time.Now()
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, 503, w.Code)
	require.Less(t, time.Since(start), time.Second)
}
func TestGracefulShutdownAllowsActiveRequestToFinish(t *testing.T) { // FND-002
	c := settings(t)
	c.ShutdownGrace = time.Second
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var canceled atomic.Bool
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		canceled.Store(r.Context().Err() != nil)
		w.WriteHeader(204)
		close(done)
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaned := make(chan struct{})
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln, func() error { close(cleaned); return nil }) }()
	response := make(chan int, 1)
	go func() {
		req, e := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/work", nil)
		if e != nil {
			return
		}
		req.SetBasicAuth("shutdown-fixture-user", "shutdown-fixture-password")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			response <- 0
			return
		}
		_ = resp.Body.Close()
		response <- resp.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not admitted")
	}
	cancel()
	require.Eventually(t, func() bool {
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
		return w.Code == 503
	}, time.Second, time.Millisecond)
	select {
	case <-cleaned:
		t.Fatal("resources closed before request finished")
	default:
	}
	close(release)
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown timed out")
	}
	require.False(t, canceled.Load())
	require.Equal(t, 204, <-response)
	<-done
	<-cleaned
}
func TestShutdownCancelsOverdueWorkAndWaitsForCleanup(t *testing.T) { // FND-002
	c := settings(t)
	c.ShutdownGrace = 30 * time.Millisecond
	c.CleanupTimeout = time.Second
	entered, finished := make(chan struct{}), make(chan struct{})
	var activeContext context.Context
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeContext = r.Context()
		close(entered)
		<-r.Context().Done()
		close(finished)
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	var cleaned atomic.Bool
	go func() {
		served <- s.Serve(ctx, ln, func() error {
			select {
			case <-finished:
				cleaned.Store(true)
			default:
				return errors.New("active work still running")
			}
			return nil
		})
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		req, e := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/work", nil)
		if e != nil {
			return
		}
		req.SetBasicAuth("shutdown-fixture-user", "shutdown-fixture-password")
		resp, e := http.DefaultClient.Do(req)
		if e == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not admitted")
	}
	start := time.Now()
	cancel()
	require.NoError(t, activeContext.Err())
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown timed out")
	}
	require.ErrorIs(t, activeContext.Err(), context.Canceled)
	require.True(t, cleaned.Load())
	require.GreaterOrEqual(t, time.Since(start), c.ShutdownGrace)
	<-clientDone
}
func TestCleanupFailureIsReported(t *testing.T) { // FND-002
	s := web.New(settings(t), slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, s.Serve(ctx, ln, func() error { return errors.New("cleanup failed") }))
}
func TestCleanupIsBounded(t *testing.T) { // FND-002
	c := settings(t)
	c.CleanupTimeout = 20 * time.Millisecond
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	err = s.Serve(ctx, ln, func() error { <-release; return nil })
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}

func TestPanicProducesSanitizedFailureLog(t *testing.T) { // SEC-001 FND-002
	var logs safeBuffer
	s := web.New(settings(t), slog.New(slog.NewJSONHandler(&logs, nil)), func(context.Context) error { return nil }, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("SECRET") }))
	w := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/work", nil)
	request.SetBasicAuth("shutdown-fixture-user", "shutdown-fixture-password")
	require.NotPanics(t, func() { s.Handler.ServeHTTP(w, request) })
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String()+logs.String(), "SECRET")
	require.Contains(t, logs.String(), "panic")
	require.Contains(t, logs.String(), `"status":500`)
}
func TestServeFailureClosesResources(t *testing.T) { // FND-002
	s := web.New(settings(t), slog.New(slog.NewJSONHandler(io.Discard, nil)), func(context.Context) error { return nil }, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	var cleaned atomic.Bool
	err = s.Serve(context.Background(), ln, func() error { cleaned.Store(true); return nil })
	require.Error(t, err)
	require.True(t, cleaned.Load())
}
