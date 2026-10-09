package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// SEC-003 SEC-002
func TestDirectInspectionOutcomeLogs(t *testing.T) {
	for _, tc := range []struct {
		name, action, failure        string
		invalidID, missing, database bool
		after                        int64
		limit                        int
	}{
		{"get_invalid_id", "read", "invalid_request", true, false, false, 0, 50},               // SEC-003
		{"history_invalid_id", "history", "invalid_request", true, false, false, 0, 50},        // SEC-003
		{"history_negative_cursor", "history", "invalid_request", false, false, false, -1, 50}, // SEC-003
		{"history_zero_limit", "history", "invalid_request", false, false, false, 0, 0},        // SEC-003
		{"history_excessive_limit", "history", "invalid_request", false, false, false, 0, 101}, // SEC-003
		{"get_not_found", "read", "not_found", false, true, false, 0, 50},                      // SEC-003
		{"history_not_found", "history", "not_found", false, true, false, 0, 50},               // SEC-003
		{"get_database_failure", "read", "database_read", false, false, true, 0, 50},           // SEC-003 SEC-002
		{"history_database_failure", "history", "database_read", false, false, true, 0, 50},    // SEC-003 SEC-002
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "open")
			r := newPolicyRepository(v)
			if tc.missing {
				r = newPolicyRepository()
			}
			if tc.database {
				r.hook = func(_ context.Context, _ string) error {
					return errors.New("sk_test_log_private 4242424242424242 CVC_LOG_SENTINEL database-private-error")
				}
			}
			var logs bytes.Buffer
			opts := policyOptions(c)
			opts.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			g := newPolicyGateway()
			s := policyService(t, r, g, opts)
			id := v.Order.ID
			if tc.invalidID {
				id = "invalid-sensitive-log-input"
			}
			before := r.State()
			var err error
			if tc.action == "read" {
				_, err = s.Get(policyCtx(), id)
			} else {
				_, err = s.History(policyCtx(), id, tc.after, tc.limit)
			}
			wantCode := tc.failure
			knownOrder := ""
			if tc.database {
				wantCode = "temporarily_unavailable"
				knownOrder = v.Order.ID
				var failure *Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, knownOrder, failure.OrderID, "log and returned known identity must agree")
				require.Empty(t, failure.OperationID, "failed read cannot invent an unloaded operation")
			}
			policyErrorCode(t, err, wantCode)
			if tc.failure == "invalid_request" {
				require.Empty(t, r.Calls(), "invalid inspection inputs cannot reach persistence")
			}
			policyAssertNoEffects(t, r, g, before)
			requireDirectOutcomeLog(t, &logs, tc.action, "failed", tc.failure, knownOrder, "", "")
		})
	}
}

// SEC-003 SEC-002 STR-008
func TestRejectedReplayLogsSavedFailure(t *testing.T) {
	for _, tc := range []struct {
		name, failure string
	}{
		{"saved_validation", "validation"}, // SEC-003 STR-008
		{"saved_credential", "credential"}, // SEC-003 STR-008
		{"saved_permission", "permission"}, // SEC-003 STR-008
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "rejected")
			v.Operation.FailureCode = policyPtr(tc.failure)
			r := newPolicyRepository(v)
			in := policyInput()
			r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
			var logs bytes.Buffer
			opts := policyOptions(c)
			opts.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			g := newPolicyGateway()
			s := policyService(t, r, g, opts)
			before := r.State()
			out, err := s.Create(policyCtx(), in)
			policyErrorCode(t, err, "checkout_rejected")
			require.True(t, out.ConfirmedRejected)
			require.Equal(t, v.Order.ID, out.Order.ID)
			require.Equal(t, v.Operation.ID, out.Operation.ID)
			require.Equal(t, v.Operation.FailureCode, out.Operation.FailureCode)
			policyAssertNoEffects(t, r, g, before)
			requireDirectOutcomeLog(t, &logs, "create", "rejected", tc.failure, v.Order.ID, v.Operation.ID, "rejected")
			require.NotContains(t, logs.String(), in.RequestKey)
			require.NotContains(t, logs.String(), v.Operation.StripeKey)
		})
	}
}

// This oracle checks the actual semantic outcome for these direct-service cases.
// It does not accept static metadata or use the permissive shared R26 helper.
func requireDirectOutcomeLog(t *testing.T, logs *bytes.Buffer, action, outcome, failure, orderID, operationID, state string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	matched := false
	for {
		var record map[string]any
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for key, known := range map[string]string{"order_id": orderID, "operation_id": operationID} {
			if actual, present := record[key]; present && actual != nil && actual != "" {
				require.NotEmpty(t, known, "cannot fabricate %s", key)
				require.Equal(t, known, actual)
			}
		}
		if record["action"] != action {
			continue
		}
		require.Equal(t, outcome, record["outcome"])
		require.Equal(t, failure, record["error_kind"])
		require.Equal(t, failure, record["failure_code"])
		if orderID != "" {
			require.Equal(t, orderID, record["order_id"])
		}
		if operationID != "" {
			require.Equal(t, operationID, record["operation_id"])
			require.Equal(t, state, record["operation_state"])
			require.Equal(t, "unpaid", record["payment_status"])
		}
		matched = true
	}
	require.True(t, matched, "direct service failure must log its actual outcome/category with known correlation")
	for _, sentinel := range []string{"sk_test_log_private", "4242424242424242", "CVC_LOG_SENTINEL", "database-private-error", "invalid-sensitive-log-input"} {
		require.NotContains(t, logs.String(), sentinel)
	}
}
