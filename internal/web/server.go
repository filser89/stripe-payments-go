// Package web owns probes, request logging, and the HTTP request lifetime.
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filser89/stripe-payments-go/internal/config"
)

type Server struct {
	Handler   http.Handler
	config    config.Config
	logger    *slog.Logger
	stopping  atomic.Bool
	admission sync.Mutex
	active    sync.WaitGroup
}

func New(c config.Config, logger *slog.Logger, probe func(context.Context) error, extra http.Handler) *Server {
	s := &Server{config: c, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), c.ReadinessTimeout)
		defer cancel()
		if err := probe(ctx); err != nil {
			logger.WarnContext(ctx, "readiness failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", errorKind(err))
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if extra != nil {
		mux.Handle("/", extra)
	}
	s.Handler = s.logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.admission.Lock()
		if s.stopping.Load() {
			s.admission.Unlock()
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		s.active.Add(1)
		s.admission.Unlock()
		defer s.active.Done()
		mux.ServeHTTP(w, r)
	}))
	return s
}

// Serve drains admitted work before canceling its contexts, then bounds cleanup.
// A non-cooperating handler or cleanup fails shutdown; the process caller exits nonzero.
func (s *Server) Serve(ctx context.Context, ln net.Listener, cleanup func() error) error {
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv := &http.Server{Handler: s.Handler, ReadHeaderTimeout: s.config.HeaderTimeout, ReadTimeout: s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout, IdleTimeout: s.config.IdleTimeout,
		BaseContext: func(net.Listener) context.Context { return requests },
		ErrorLog:    slog.NewLogLogger(s.logger.Handler(), slog.LevelError),
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-served:
	}
	s.admission.Lock()
	s.stopping.Store(true)
	s.admission.Unlock()
	grace, cancelGrace := context.WithTimeout(context.Background(), s.config.ShutdownGrace)
	shutdownErr := srv.Shutdown(grace)
	cancelGrace()
	cancelRequests()
	if shutdownErr != nil {
		if err := srv.Close(); err != nil {
			s.logger.Error("HTTP close failed", "error_kind", "http_close")
		}
	}
	cleaned := make(chan error, 1)
	go func() { s.active.Wait(); cleaned <- cleanup() }()
	timer := time.NewTimer(s.config.CleanupTimeout)
	defer timer.Stop()
	select {
	case err := <-cleaned:
		if err != nil {
			return fmt.Errorf("resource cleanup failed")
		}
	case <-timer.C:
		return fmt.Errorf("resource cleanup deadline exceeded")
	}
	if serveErr == nil {
		serveErr = <-served
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("HTTP serving failed")
	}
	return nil
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var raw [16]byte
		_, _ = rand.Read(raw[:])
		id := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-ID", id)
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "request failed", "request_id", id, "error_kind", "panic")
				if rw.status == 0 {
					http.Error(rw, "internal error", http.StatusInternalServerError)
				}
			}
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			// Raw paths, query strings, headers, and bodies can contain secrets.
			s.logger.InfoContext(r.Context(), "request completed", "request_id", id, "status", status, "duration_ms", float64(time.Since(start).Microseconds())/1000)
		}()
		next.ServeHTTP(rw, r)
	})
}
func errorKind(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "database_unavailable"
}
