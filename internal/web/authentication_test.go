package web_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// admissionBody observes application reads without involving transport draining.
type admissionBody struct {
	reads atomic.Int32
}

func (b *admissionBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return copy(p, "protected-body"), io.EOF
}

func (*admissionBody) Close() error { return nil }

func TestAuthenticationShutdownAdmission(t *testing.T) { // FND-002 V1
	var calls atomic.Int32
	s := web.New(settings(t), slog.New(slog.NewJSONHandler(io.Discard, nil)),
		func(context.Context) error { return nil },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cleanupStarted, release := make(chan struct{}), make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- s.Serve(ctx, ln, func() error {
			close(cleanupStarted)
			<-release
			return nil
		})
	}()
	t.Cleanup(func() {
		close(release)
		select {
		case err := <-served:
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Error("shutdown did not finish after cleanup release")
		}
	})
	select {
	case <-cleanupStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not reach cleanup")
	}
	// Cleanup starts only after admission closes. Both requests therefore overlap
	// the shutdown lifecycle without using a sleep to infer its state.
	for _, authenticated := range []bool{false, true} {
		body := &admissionBody{}
		r := httptest.NewRequest(http.MethodGet, "/work", body)
		if authenticated {
			r.SetBasicAuth("shutdown-fixture-user", "shutdown-fixture-password")
		}
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code, "authenticated=%t", authenticated)
		assert.Zero(t, body.reads.Load(), "shutdown rejection must not read protected input")
		assert.Zero(t, calls.Load(), "shutdown rejection must not invoke protected work")
	}
}

const authUser = "boundary-fixture-user"
const authPassword = " boundary:fixture-password "

func authSettings(t *testing.T, username, password string) config.Config {
	t.Helper()
	c, err := config.Load(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://fixture:database-secret@localhost/fixture"
		case "BASIC_AUTH_USERNAME":
			return username
		case "BASIC_AUTH_PASSWORD":
			return password
		default:
			return ""
		}
	})
	require.NoError(t, err)
	return c
}

func authServer(t *testing.T, username, password string, extra http.Handler) (*web.Server, *safeBuffer) {
	t.Helper()
	logs := &safeBuffer{}
	return web.New(authSettings(t, username, password), slog.New(slog.NewJSONHandler(logs, nil)),
		func(context.Context) error { return nil }, extra), logs
}

func authHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func authRequest(method, path string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, path, body)
	r.SetBasicAuth(authUser, authPassword)
	return r
}

func assertUnauthorized(t *testing.T, w *httptest.ResponseRecorder, method string) {
	t.Helper()
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, `Basic realm="stripe-payments"`, w.Header().Get("WWW-Authenticate"))
	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	if method == http.MethodHead {
		assert.Empty(t, w.Body.String())
	} else {
		assert.Equal(t, "Unauthorized", strings.TrimSpace(w.Body.String()))
	}
}

func assertBadBody(t *testing.T, w *httptest.ResponseRecorder, method string) {
	t.Helper()
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	if method == http.MethodHead {
		assert.Empty(t, w.Body.String())
	} else {
		assert.NotEmpty(t, strings.TrimSpace(w.Body.String()))
		assert.NotContains(t, w.Body.String(), "sensitive-body-sentinel")
		assert.NotContains(t, w.Body.String(), "sensitive-read-error-sentinel")
	}
}

func TestAuthenticationMissingCredentials(t *testing.T) { // AUTH-001 V1–V4, BRW-001 V2–V3
	var calls atomic.Int32
	s, _ := authServer(t, authUser, authPassword, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, afterSuccess := range []bool{false, true} {
			if afterSuccess {
				w := httptest.NewRecorder()
				before := calls.Load()
				s.Handler.ServeHTTP(w, authRequest(method, "/work", nil))
				assert.Equal(t, http.StatusNoContent, w.Code)
				assert.Equal(t, before+1, calls.Load())
				assert.Empty(t, w.Header().Values("Set-Cookie"))
			}
			body := &admissionBody{}
			before := calls.Load()
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest(method, "/work", body))
			assertUnauthorized(t, w, method)
			assert.Equal(t, before, calls.Load())
			assert.Zero(t, body.reads.Load())
		}
	}
}

func TestAuthenticationExactCredentials(t *testing.T) { // AUTH-002 V1–V7
	for _, tc := range []struct {
		name, configuredUser, configuredPassword, submittedUser, submittedPassword string
		accepted                                                                   bool
	}{
		{"exact", authUser, authPassword, authUser, authPassword, true},
		{"wrong_username", authUser, authPassword, "wrong-user", authPassword, false},                                                     // V1
		{"wrong_password", authUser, authPassword, authUser, "wrong-password", false},                                                     // V2
		{"both_wrong", authUser, authPassword, "wrong-user", "wrong-password", false},                                                     // V3
		{"username_case", authUser, authPassword, strings.ToUpper(authUser), authPassword, false},                                         // V4
		{"password_case", authUser, authPassword, authUser, strings.ToUpper(authPassword), false},                                         // V4
		{"leading_space_significant", authUser, authPassword, authUser, strings.TrimLeft(authPassword, " "), false},                       // V5
		{"trailing_space_significant", authUser, authPassword, authUser, strings.TrimRight(authPassword, " "), false},                     // V5
		{"minimum_lengths", "!", " ", "!", " ", true},                                                                                     // V6
		{"maximum_lengths", strings.Repeat("~", 128), strings.Repeat("~", 256), strings.Repeat("~", 128), strings.Repeat("~", 256), true}, // V6
		{"username_not_trimmed", authUser, authPassword, " " + authUser, authPassword, false},                                             // V7
		{"password_not_trimmed", authUser, "password", authUser, " password ", false},                                                     // V7
	} {
		var calls atomic.Int32
		s, _ := authServer(t, tc.configuredUser, tc.configuredPassword, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		body := &admissionBody{}
		r := httptest.NewRequest(http.MethodGet, "/work", body)
		r.SetBasicAuth(tc.submittedUser, tc.submittedPassword)
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		if tc.accepted {
			assert.Equal(t, http.StatusNoContent, w.Code, tc.name)
			assert.EqualValues(t, 1, calls.Load(), tc.name)
		} else {
			assertUnauthorized(t, w, http.MethodGet)
			assert.Zero(t, calls.Load(), tc.name)
			assert.Zero(t, body.reads.Load(), tc.name)
		}
	}
}

func TestAuthenticationAuthorizationParsing(t *testing.T) { // AUTH-003 V1–V12
	token := base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword))
	for _, tc := range []struct {
		name     string
		headers  []string
		accepted bool
	}{
		{"empty_field", []string{""}, false},                                                                               // V1
		{"scheme_without_token", []string{"Basic"}, false},                                                                 // V2
		{"empty_token", []string{"Basic "}, false},                                                                         // V2
		{"unsupported_scheme", []string{"Bearer " + token}, false},                                                         // V3
		{"malformed_base64", []string{"Basic !!encoded-secret!!"}, false},                                                  // V4
		{"missing_colon", []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(authUser))}, false},                 // V5
		{"empty_username", []string{authHeader("", authPassword)}, false},                                                  // V6
		{"empty_password", []string{authHeader(authUser, "")}, false},                                                      // V6
		{"username_control", []string{authHeader("bad\tuser", authPassword)}, false},                                       // V7
		{"password_control", []string{authHeader(authUser, "bad\npassword")}, false},                                       // V7
		{"username_non_ascii", []string{authHeader("caf\xc3\xa9", authPassword)}, false},                                   // V7
		{"password_non_ascii", []string{authHeader(authUser, "caf\xc3\xa9")}, false},                                       // V7
		{"invalid_utf8", []string{authHeader(authUser, "\xff")}, false},                                                    // V7
		{"repeated_valid_fields", []string{authHeader(authUser, authPassword), authHeader(authUser, authPassword)}, false}, // V8
		{"valid_then_invalid", []string{authHeader(authUser, authPassword), "Bearer duplicate-secret"}, false},             // V8
		{"invalid_then_valid", []string{"Bearer duplicate-secret", authHeader(authUser, authPassword)}, false},             // V8
		{"mixed_case_scheme", []string{"bAsIc " + token}, true},                                                            // V9
		{"password_colon_retained", []string{authHeader(authUser, authPassword)}, true},                                    // V10
		{"matching_at_limit", []string{"Basic" + strings.Repeat(" ", 4096-len("Basic")-len(token)) + token}, true},         // V11
		{"malformed_at_limit", []string{"Basic " + strings.Repeat("!", 4090)}, false},                                      // V11
		{"wrong_within_limit", []string{authHeader(authUser, "wrong")}, false},                                             // V11
		{"matching_over_limit", []string{"Basic" + strings.Repeat(" ", 4097-len("Basic")-len(token)) + token}, false},      // V12
	} {
		var calls atomic.Int32
		s, logs := authServer(t, authUser, authPassword, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		body := &admissionBody{}
		r := httptest.NewRequest(http.MethodGet, "/work", body)
		for _, header := range tc.headers {
			r.Header.Add("Authorization", header)
		}
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		if tc.accepted {
			assert.Equal(t, http.StatusNoContent, w.Code, tc.name)
			assert.EqualValues(t, 1, calls.Load(), tc.name)
		} else {
			assertUnauthorized(t, w, http.MethodGet)
			assert.Zero(t, calls.Load(), tc.name)
			assert.Zero(t, body.reads.Load(), tc.name)
		}
		assert.NotContains(t, logs.String()+w.Body.String(), authPassword, tc.name)
		assert.NotContains(t, logs.String()+w.Body.String(), token, tc.name)
	}
}

func TestAuthenticationCredentialSources(t *testing.T) { // AUTH-004 V1–V4
	var calls atomic.Int32
	s, _ := authServer(t, authUser, authPassword, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, source := range []string{"query", "cookie", "body"} {
		body := &observedBody{reader: strings.NewReader("username=" + authUser + "&password=" + authPassword)}
		r := httptest.NewRequest(http.MethodPost, "/work", body)
		switch source {
		case "query":
			q := r.URL.Query()
			q.Set("username", authUser)
			q.Set("password", authPassword)
			r.URL.RawQuery = q.Encode()
		case "cookie":
			r.AddCookie(&http.Cookie{Name: "username", Value: authUser})
			r.AddCookie(&http.Cookie{Name: "password", Value: authPassword})
			r.AddCookie(&http.Cookie{Name: "Authorization", Value: authHeader(authUser, authPassword)})
		case "body":
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		assertUnauthorized(t, w, http.MethodPost)
		assert.Zero(t, body.reads.Load(), source)
		assert.Zero(t, calls.Load(), source)
	}
}

func TestAuthenticationDelegation(t *testing.T) { // AUTH-005 V1–V4
	for _, status := range []int{http.StatusCreated, http.StatusUnprocessableEntity} {
		var calls atomic.Int32
		var method, path, query, received string
		var readErr error
		s, _ := authServer(t, authUser, authPassword, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
			data, err := io.ReadAll(r.Body)
			received, readErr = string(data), err
			w.Header().Set("X-Handler-Result", "preserved")
			w.WriteHeader(status)
			_, err = io.WriteString(w, "handler-result")
			assert.NoError(t, err)
		}))
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, authRequest(http.MethodPost, "/work", strings.NewReader("handler-input")))
		assert.EqualValues(t, 1, calls.Load())
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/work", path)
		assert.Empty(t, query)
		assert.Equal(t, "handler-input", received)
		assert.NoError(t, readErr)
		assert.Equal(t, status, w.Code)
		assert.Equal(t, "preserved", w.Header().Get("X-Handler-Result"))
		assert.Equal(t, "handler-result", w.Body.String())
		assert.Empty(t, w.Header().Get("WWW-Authenticate"))
		assert.Empty(t, w.Header().Values("Set-Cookie"))
		// A nonempty query survives the same real server boundary as the body.
		w = httptest.NewRecorder()
		s.Handler.ServeHTTP(w, authRequest(http.MethodPut, "/work?x=1&x=2", strings.NewReader("second-input")))
		assert.EqualValues(t, 2, calls.Load())
		assert.Equal(t, http.MethodPut, method)
		assert.Equal(t, "/work", path)
		assert.Equal(t, "x=1&x=2", query)
		assert.Equal(t, "second-input", received)
		assert.Equal(t, status, w.Code)
		assert.Equal(t, "handler-result", w.Body.String())
	}
}

func TestAuthenticationConcurrentIsolation(t *testing.T) { // AUTH-006 V1–V3
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s, _ := authServer(t, authUser, authPassword, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	var workers sync.WaitGroup
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); workers.Wait() })
	// Admission of the valid caller provides an explicit overlap barrier.
	validDone := make(chan *httptest.ResponseRecorder, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, authRequest(http.MethodGet, "/work", nil))
		validDone <- w
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("valid request did not enter handler")
	}
	for _, header := range []string{"", authHeader(authUser, "wrong")} {
		body := &admissionBody{}
		r := httptest.NewRequest(http.MethodGet, "/work", body)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		finished := make(chan *httptest.ResponseRecorder, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, r)
			finished <- w
		}()
		select {
		case w := <-finished:
			assertUnauthorized(t, w, http.MethodGet)
			assert.Zero(t, body.reads.Load())
		case <-time.After(time.Second):
			t.Error("rejected caller entered protected work or waited on another caller")
		}
	}
	assert.EqualValues(t, 1, calls.Load())
	select {
	case <-validDone:
		t.Error("valid work finished before release")
	default:
	}
	unblock()
	select {
	case w := <-validDone:
		assert.Equal(t, http.StatusNoContent, w.Code)
	case <-time.After(time.Second):
		t.Error("valid work did not finish after release")
	}
}

func TestAuthenticationLanding(t *testing.T) { // HTTP-001 V1–V4
	s, _ := authServer(t, authUser, authPassword, nil)
	get := httptest.NewRecorder()
	s.Handler.ServeHTTP(get, authRequest(http.MethodGet, "/?view=local", nil))
	assert.Equal(t, http.StatusOK, get.Code)
	assert.Contains(t, get.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, strings.ToLower(get.Body.String()), "payment")
	assert.Less(t, get.Body.Len(), 16*1024)
	for _, forbidden := range []string{authUser, authPassword, "database-secret", "<form", "<input", "<button", "checkout", "sk_test_", "whsec_"} {
		assert.NotContains(t, get.Body.String(), forbidden)
	}
	assert.Empty(t, get.Header().Values("Set-Cookie"))
	head := httptest.NewRecorder()
	s.Handler.ServeHTTP(head, authRequest(http.MethodHead, "/?view=local", nil))
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Empty(t, head.Body.String())
	assert.Equal(t, get.Header().Get("Content-Type"), head.Header().Get("Content-Type"))
	assert.Equal(t, get.Header().Get("Cache-Control"), head.Header().Get("Cache-Control"))
	assert.Equal(t, get.Header().Get("Content-Length"), head.Header().Get("Content-Length"))
}

func TestAuthenticationRouting(t *testing.T) { // HTTP-002 V1–V4
	s, _ := authServer(t, authUser, authPassword, nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/", http.StatusMethodNotAllowed},
		{http.MethodPut, "/", http.StatusMethodNotAllowed},
		{http.MethodGet, "/unknown", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, authRequest(tc.method, tc.path, nil))
		assert.Equal(t, tc.status, w.Code)
		body := &admissionBody{}
		w = httptest.NewRecorder()
		s.Handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, body))
		assertUnauthorized(t, w, tc.method)
		assert.Zero(t, body.reads.Load())
	}
}

type observedBody struct {
	reader io.Reader
	reads  atomic.Int32
	bytes  atomic.Int32
}

func (b *observedBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	n, err := b.reader.Read(p)
	b.bytes.Add(int32(n))
	return n, err
}
func (*observedBody) Close() error { return nil }

type failingBody struct{ withByte bool }

func (b failingBody) Read(p []byte) (int, error) {
	if b.withByte {
		return copy(p, "x"), errors.New("sensitive-read-error-sentinel")
	}
	return 0, errors.New("sensitive-read-error-sentinel")
}

func TestAuthenticationLandingBodyBytes(t *testing.T) { // HTTP-003 V1–V5
	s, _ := authServer(t, authUser, authPassword, nil)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, tc := range []struct {
			name     string
			reader   io.Reader
			declared int64
			accepted bool
		}{
			{"nil", nil, 0, true},                                                                                // V1
			{"ordinary_empty", strings.NewReader(""), 0, true},                                                   // V1
			{"byte_empty_eof", &observedBody{reader: strings.NewReader("")}, -1, true},                           // V2
			{"immediate_byte", &observedBody{reader: strings.NewReader("x")}, 1, false},                          // V3
			{"large_body", &observedBody{reader: strings.NewReader(strings.Repeat("x", 1<<20))}, 1 << 20, false}, // V3
			{"declared_zero_actual_byte", &observedBody{reader: strings.NewReader("x")}, 0, false},               // V4
			{"declared_positive_actual_empty", &observedBody{reader: strings.NewReader("")}, 1, true},            // V4
			{"unknown_length_actual_byte", &observedBody{reader: strings.NewReader("x")}, -1, false},             // V4
		} {
			r := authRequest(method, "/", tc.reader)
			r.ContentLength = tc.declared
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, r)
			if tc.accepted {
				assert.Equal(t, http.StatusOK, w.Code, tc.name)
			} else {
				assertBadBody(t, w, method)
			}
			if method == http.MethodHead {
				assert.Empty(t, w.Body.String(), tc.name)
			} // V5
			if body, ok := tc.reader.(*observedBody); ok {
				assert.Positive(t, body.reads.Load(), tc.name)
				// The large fixture must not be consumed in full. This allows
				// finite inspection buffers without prescribing their size.
				assert.Less(t, body.bytes.Load(), int32(1<<20), "landing must bound actual-byte inspection: %s", tc.name)
			}
		}
	}
}

func TestAuthenticationBodyReadFailure(t *testing.T) { // HTTP-004 V1–V4
	s, logs := authServer(t, authUser, authPassword, nil)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, withByte := range []bool{false, true} {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, authRequest(method, "/", failingBody{withByte: withByte}))
			assertBadBody(t, w, method)
			assert.NotContains(t, w.Body.String()+logs.String(), "sensitive-read-error-sentinel")
		}
	}
	// This test uses an operating response writer. Connection failure and shutdown
	// cannot promise a delivered 400; their witnesses are the real transport tests
	// and TestAuthenticationShutdownAdmission, respectively (V4).
}

func TestAuthenticationPublicProbeBoundary(t *testing.T) { // FND-001 V1–V5
	var calls atomic.Int32
	var unavailable atomic.Bool
	var protected atomic.Int32
	c := authSettings(t, authUser, authPassword)
	c.ReadinessTimeout = 20 * time.Millisecond
	s := web.New(c, slog.New(slog.NewJSONHandler(io.Discard, nil)), func(ctx context.Context) error {
		calls.Add(1)
		if unavailable.Load() {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { protected.Add(1); w.WriteHeader(http.StatusNoContent) }))
	for _, path := range []string{"/healthz", "/readyz"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Empty(t, w.Header().Get("WWW-Authenticate"))
			if method == http.MethodHead {
				assert.Empty(t, w.Body.String())
			}
		}
	}
	assert.EqualValues(t, 2, calls.Load())
	unavailable.Store(true)
	start := time.Now()
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Less(t, time.Since(start), time.Second)
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	unavailable.Store(false)
	w = httptest.NewRecorder()
	s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	for _, path := range []string{"/healthz", "/readyz"} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		}
	}
	for _, path := range []string{"/healthz/private", "/readyz/private", "/healthz-extra", "/readyz-extra"} {
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assertUnauthorized(t, w, http.MethodGet)
	}
	assert.Zero(t, protected.Load())
}

func TestAuthenticationSanitizedOutcomes(t *testing.T) { // SEC-001 V2–V8; V1 is command startup evidence
	for _, tc := range []struct {
		name, method string
		headers      []string
		body         io.Reader
		status       int
	}{
		{"accepted", http.MethodGet, []string{authHeader(authUser, authPassword)}, nil, http.StatusOK},                                                                                    // V2
		{"missing", http.MethodGet, nil, nil, http.StatusUnauthorized},                                                                                                                    // V3
		{"wrong", http.MethodGet, []string{authHeader(authUser, "submitted-wrong-secret")}, nil, http.StatusUnauthorized},                                                                 // V3
		{"malformed", http.MethodGet, []string{"Basic malformed-authorization-secret"}, nil, http.StatusUnauthorized},                                                                     // V3
		{"duplicate", http.MethodGet, []string{authHeader(authUser, authPassword), authHeader(authUser, authPassword)}, nil, http.StatusUnauthorized},                                     // V3
		{"oversize", http.MethodGet, []string{"Basic " + strings.Repeat(" ", 4097) + base64.StdEncoding.EncodeToString([]byte(authUser+":"+authPassword))}, nil, http.StatusUnauthorized}, // V3
		{"body", http.MethodGet, []string{authHeader(authUser, authPassword)}, strings.NewReader("sensitive-body-sentinel"), http.StatusBadRequest},                                       // V4
		{"read_failure", http.MethodGet, []string{authHeader(authUser, authPassword)}, failingBody{}, http.StatusBadRequest},                                                              // V5
		{"head_auth", http.MethodHead, nil, nil, http.StatusUnauthorized},                                                                                                                 // V6
		{"head_body", http.MethodHead, []string{authHeader(authUser, authPassword)}, strings.NewReader("sensitive-body-sentinel"), http.StatusBadRequest},                                 // V6
		{"head_read_failure", http.MethodHead, []string{authHeader(authUser, authPassword)}, failingBody{}, http.StatusBadRequest},                                                        // V6
	} {
		s, logs := authServer(t, authUser, authPassword, nil)
		r := httptest.NewRequest(tc.method, "/?password=query-secret-sentinel", tc.body)
		r.Header.Set("X-Request-ID", "untrusted-request-id-secret")
		for _, header := range tc.headers {
			r.Header.Add("Authorization", header)
		}
		w := httptest.NewRecorder()
		s.Handler.ServeHTTP(w, r)
		assert.Equal(t, tc.status, w.Code, tc.name)
		if tc.status == http.StatusUnauthorized {
			assertUnauthorized(t, w, tc.method)
		}
		if tc.status == http.StatusBadRequest {
			assertBadBody(t, w, tc.method)
		}
		id := w.Header().Get("X-Request-ID")
		assert.NotEmpty(t, id)
		assert.NotEqual(t, "untrusted-request-id-secret", id)
		combined := logs.String() + w.Body.String() + fmt.Sprint(w.Header())
		forbidden := []string{authUser, authPassword, base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword)), "database-secret", "submitted-wrong-secret", "malformed-authorization-secret", "sensitive-body-sentinel", "sensitive-read-error-sentinel", "query-secret-sentinel", "untrusted-request-id-secret"}
		for _, header := range tc.headers {
			forbidden = append(forbidden, header)
			parts := strings.Fields(header)
			if len(parts) == 2 {
				forbidden = append(forbidden, parts[1])
				if decoded, err := base64.StdEncoding.DecodeString(parts[1]); err == nil {
					forbidden = append(forbidden, string(decoded))
					if username, password, ok := strings.Cut(string(decoded), ":"); ok {
						forbidden = append(forbidden, username, password)
					}
				}
			}
		}
		var completed bool
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry map[string]any
			if !assert.NoError(t, json.Unmarshal([]byte(line), &entry), tc.name) {
				continue
			}
			// Include decoded structured values, including nested attributes,
			// rather than inspecting only their escaped JSON serialization.
			combined += "\n" + fmt.Sprint(entry)
			if entry["msg"] == "request completed" {
				completed = true
				assert.Equal(t, id, entry["request_id"])
				assert.EqualValues(t, tc.status, entry["status"])
				assert.Contains(t, entry, "duration_ms")
			}
		}
		for _, secret := range forbidden {
			assert.NotContains(t, combined, secret, tc.name)
		}
		assert.True(t, completed, "missing structured completion outcome: %s", tc.name)
	}
}
