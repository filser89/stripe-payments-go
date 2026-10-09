package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/filser89/stripe-payments-go/internal/config"
)

// Account digests are immutable for the handler's lifetime. Invalid construction
// fails closed, including callers that bypass the serving entry point.
func authenticate(c config.Config, logger *slog.Logger, next http.Handler) http.Handler {
	valid := c.ValidateBasic() == nil
	username := sha256.Sum256([]byte(c.BasicAuthUsername))
	password := sha256.Sum256([]byte(c.BasicAuthPassword))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := basicCredentials(r)
		if ok && valid {
			candidateUser := sha256.Sum256([]byte(user))
			candidatePass := sha256.Sum256([]byte(pass))
			// Both fixed-size comparisons execute even when one credential differs.
			userMatch := subtle.ConstantTimeCompare(username[:], candidateUser[:])
			passMatch := subtle.ConstantTimeCompare(password[:], candidatePass[:])
			if userMatch&passMatch == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		logger.InfoContext(r.Context(), "authentication rejected", "action", "authenticate", "outcome", "rejected", "error_kind", "auth_rejected", "status", http.StatusUnauthorized, "request_id", w.Header().Get("X-Request-ID"))
		w.Header().Set("WWW-Authenticate", `Basic realm="stripe-payments"`)
		plainResponse(w, r, logger, http.StatusUnauthorized, "Unauthorized")
	})
}

func basicCredentials(r *http.Request) (string, string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 4096 {
		return "", "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	// The byte length excludes non-ASCII Unicode case-fold equivalents.
	if !ok || len(scheme) != len("Basic") || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	token = strings.TrimLeft(token, " ")
	// DecodeString ignores CR/LF; the Basic token grammar does not permit them.
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return "", "", false
	}
	return username, password, true
}

func plainResponse(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, message string) {
	body := message + "\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		if _, err := io.WriteString(w, body); err != nil {
			logger.WarnContext(r.Context(), "response write failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "response_write")
		}
	}
}
