package testutil

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type Logs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *Logs) Contents() string     { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }
func (l *Logs) Logger() *slog.Logger { return slog.New(slog.NewJSONHandler(l, nil)) }
func (l *Logs) Records() ([]map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := json.NewDecoder(bytes.NewReader(l.buffer.Bytes()))
	var out []map[string]any
	for d.More() {
		var v map[string]any
		if e := d.Decode(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

// OutcomeLogContext accepts useful structured outcome/error fields or an outcome
// message with the actual HTTP status. Generic request telemetry and IDs alone
// cannot establish feature evidence. Category/message wording is unconstrained.
func OutcomeLogContext(record map[string]any, status int) bool {
	statusAgrees := false
	if v, ok := record["status"]; ok {
		switch v := v.(type) {
		case float64:
			statusAgrees = v == float64(status)
		case int:
			statusAgrees = v == status
		case string:
			if strings.TrimSpace(v) != "" {
				return true
			}
		}
	}
	for key, value := range record {
		switch key {
		case "request_id", "order_id", "operation_id", "time", "level", "duration_ms", "status", "method", "path", "route", "msg", "stripe_request_id", "session_id", "payment_intent_id", "event_id":
			continue
		}
		// Structured result, state, category, error-kind or nested error context
		// carries evidence independently of the chosen field/category spelling.
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return true
		}
		if fields, ok := value.(map[string]any); ok && len(fields) > 0 {
			return true
		}
	}
	msg, _ := record["msg"].(string)
	msg = strings.TrimSpace(msg)
	return statusAgrees && msg != "" && msg != "request completed"
}

// RequireRejectedLog checks actual feature rejection records. No durable work
// is accepted on these paths, so any durable IDs must come from known fixtures.
func RequireRejectedLog(t *testing.T, logs *Logs, requestID string, status int, knownOrderID, knownOperationID string, submitted ...string) {
	t.Helper()
	require.NotEmpty(t, requestID)
	records, err := logs.Records()
	require.NoError(t, err)
	found := false
	for _, record := range records {
		if record["request_id"] != requestID {
			continue
		}
		for key, known := range map[string]string{"order_id": knownOrderID, "operation_id": knownOperationID} {
			if value, exists := record[key]; exists && value != nil && value != "" {
				require.NotEmpty(t, known, "rejection must not invent %s", key)
				require.Equal(t, known, value, "rejection must not invent %s", key)
			}
		}
		if OutcomeLogContext(record, status) {
			found = true
		}
	}
	require.True(t, found, "actual feature rejection must log sanitized structured outcome/context with response request ID")
	sentinels := []string{"checkout-fixture-password", "wrong-password", "sk_test_fixture", "test-only-fixture", "sensitive-sentinel", "4242424242424242", "CVC_SENTINEL", "123-cvc", "read-sentinel", base64.StdEncoding.EncodeToString([]byte("checkout-fixture-user:checkout-fixture-password")), base64.StdEncoding.EncodeToString([]byte("checkout-fixture-user:wrong")), base64.StdEncoding.EncodeToString([]byte("checkout-fixture-user:wrong-password"))}
	for _, body := range submitted {
		// Tiny tokens also occur in ordinary JSON/log field names. Whole submitted
		// bodies are checked when they can be distinguished from that telemetry.
		if len(body) > 8 {
			sentinels = append(sentinels, body)
		}
	}
	for _, sentinel := range sentinels {
		require.NotContains(t, logs.Contents(), sentinel)
	}
}
