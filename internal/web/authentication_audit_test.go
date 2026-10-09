package web_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthenticationAuditASCIISchemeOnWire(t *testing.T) {
	address := startTransport(t, authSettings(t, authUser, authPassword), nil)
	token := base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword))
	for _, tc := range []struct {
		name, scheme string
		status       int
	}{
		{"ascii", "Basic", http.StatusOK},
		{"ascii_mixed_case", "bAsIc", http.StatusOK},
		{"unicode_long_s", "Ba\u017fic", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := transportConn(t, address)
			// A wire request proves the transport admits this header value and
			// leaves the ASCII authentication-scheme decision to the application.
			_, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nAuthorization: %s %s\r\nConnection: close\r\n\r\n", tc.scheme, token)
			require.NoError(t, err)
			response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, tc.status, response.StatusCode)
			if tc.status == http.StatusUnauthorized {
				assert.Equal(t, `Basic realm="stripe-payments"`, response.Header.Get("WWW-Authenticate"))
				assert.Equal(t, "text/plain; charset=utf-8", response.Header.Get("Content-Type"))
				assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
				assert.Equal(t, "Unauthorized", strings.TrimSpace(string(body)))
			}
			for _, secret := range []string{authUser, authPassword, token} {
				assert.NotContains(t, string(body)+fmt.Sprint(response.Header), secret)
			}
		})
	}
}

func TestAuthenticationAuditLandingReadFailureContext(t *testing.T) {
	for _, tc := range []struct {
		name        string
		method      string
		body        io.Reader
		status      int
		readFailure bool
	}{
		{"GET/zero_byte_read_failure", http.MethodGet, failingBody{}, http.StatusBadRequest, true},
		{"GET/nonempty_body", http.MethodGet, strings.NewReader("sensitive-body-sentinel"), http.StatusBadRequest, false},
		{"GET/clean_eof", http.MethodGet, strings.NewReader(""), http.StatusOK, false},
		{"HEAD/zero_byte_read_failure", http.MethodHead, failingBody{}, http.StatusBadRequest, true},
		{"HEAD/nonempty_body", http.MethodHead, strings.NewReader("sensitive-body-sentinel"), http.StatusBadRequest, false},
		{"HEAD/clean_eof", http.MethodHead, strings.NewReader(""), http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, logs := authServer(t, authUser, authPassword, nil)
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, authRequest(tc.method, "/", tc.body))
			assert.Equal(t, tc.status, w.Code)
			if tc.status == http.StatusBadRequest {
				assertBadBody(t, w, tc.method)
				if tc.method == http.MethodGet {
					assert.Equal(t, "Bad Request", strings.TrimSpace(w.Body.String()))
				}
			}
			id := w.Header().Get("X-Request-ID")
			require.NotEmpty(t, id)
			combined := logs.String() + w.Body.String() + fmt.Sprint(w.Header())
			completionCount, readFailureCount := 0, 0
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &entry))
				combined += fmt.Sprint(entry)
				if entry["msg"] == "request completed" {
					completionCount++
					assert.Equal(t, id, entry["request_id"])
					assert.EqualValues(t, tc.status, entry["status"])
				}
				// Accept a descriptive category such as body_read or
				// request_body_read_failed without requiring a log message,
				// severity, or separate event from the completion record.
				kind, _ := entry["error_kind"].(string)
				kind = strings.ToLower(kind)
				if strings.Contains(kind, "read") && (strings.Contains(kind, "body") || strings.Contains(kind, "input")) {
					readFailureCount++
					assert.Equal(t, id, entry["request_id"])
				}
			}
			assert.Equal(t, 1, completionCount, "retain one structured request outcome")
			wantReadFailures := 0
			if tc.readFailure {
				wantReadFailures = 1
			}
			assert.Equal(t, wantReadFailures, readFailureCount, "read errors need one correlated safe category distinct from payload rejection and clean EOF")
			for _, secret := range []string{authUser, authPassword, authHeader(authUser, authPassword), base64.StdEncoding.EncodeToString([]byte(authUser + ":" + authPassword)), "sensitive-read-error-sentinel", "sensitive-body-sentinel"} {
				assert.NotContains(t, combined, secret)
			}
		})
	}
}
