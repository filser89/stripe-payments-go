package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSTR004RecoveryRules(t *testing.T) { // STR-004
	cases := []struct {
		name     string
		boundary string
	}{
		{"parent_cancel_during_external_call", "gateway"},                // STR-004
		{"parent_cancel_during_database_acceptance", "AcceptInitial"},    // STR-004
		{"parent_cancel_during_final_result_commit", "ApplyObservation"}, // STR-004
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			ctx, cancel := context.WithCancel(policyCtx())
			defer cancel()
			entered := make(chan struct{})
			exited := make(chan struct{})
			g := newPolicyGateway(policyOpenReply())
			if tc.boundary == "gateway" {
				g = newPolicyGateway(policyReply{run: func(callctx context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
					close(entered)
					<-callctx.Done()
					close(exited)
					return SessionEvidence{ErrorClass: "transport"}, callctx.Err()
				}})
			} else {
				r.hook = func(callctx context.Context, method string) error {
					if method != tc.boundary {
						return nil
					}
					close(entered)
					<-callctx.Done()
					close(exited)
					return callctx.Err()
				}
			}
			s := policyService(t, r, g, policyOptions(c))
			done := make(chan struct{})
			go func() { defer close(done); _, _ = s.Create(ctx, policyInput()) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("operation did not enter selected cancellable boundary")
			}
			cancel()
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("dependency work did not exit after cancellation")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("payment operation did not finish after owned work exited")
			}
			st := r.State()
			if tc.boundary == "AcceptInitial" {
				require.Empty(t, st.Orders)
				require.Empty(t, g.Calls())
			} else {
				require.Len(t, st.Orders, 1)
				require.Len(t, st.Operations, 1)
				for _, op := range st.Operations {
					require.Equal(t, "unresolved", op.State)
					require.NotNil(t, op.FirstDispatchAt)
				}
				require.Len(t, g.Calls(), 1)
			}
		})
	}
	t.Run("earlier_parent_deadline_caps_call_and_owned_work_exits", func(t *testing.T) { // STR-004
		c := newPolicyClock()
		r := newPolicyRepository()
		ctx, cancel := context.WithTimeout(policyCtx(), 30*time.Millisecond)
		defer cancel()
		parentEnd, ok := ctx.Deadline()
		require.True(t, ok)
		exited := make(chan struct{})
		g := newPolicyGateway(policyReply{run: func(callctx context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
			end, has := callctx.Deadline()
			require.True(t, has)
			require.False(t, end.After(parentEnd))
			<-callctx.Done()
			close(exited)
			return SessionEvidence{ErrorClass: "transport"}, callctx.Err()
		}})
		opts := policyOptions(c)
		opts.MaxAttempts = 1
		s := policyService(t, r, g, opts)
		start := time.Now()
		_, _ = s.Create(ctx, policyInput())
		require.Less(t, time.Since(start), time.Second)
		select {
		case <-exited:
		default:
			t.Fatal("response returned before owned external call exited")
		}
		require.Len(t, g.Calls(), 1)
		for _, op := range r.State().Operations {
			require.Equal(t, "unresolved", op.State)
		}
	})
	t.Run("per_call_timeout_bounds_hung_gateway", func(t *testing.T) { // STR-004
		c := newPolicyClock()
		r := newPolicyRepository()
		exited := make(chan struct{})
		g := newPolicyGateway(policyReply{run: func(ctx context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
			<-ctx.Done()
			close(exited)
			return SessionEvidence{ErrorClass: "transport"}, ctx.Err()
		}})
		opts := policyOptions(c)
		opts.CallTimeout = 100 * time.Millisecond
		opts.MaxAttempts = 1
		s := policyService(t, r, g, opts)
		start := time.Now()
		out, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		require.Less(t, time.Since(start), time.Second)
		require.GreaterOrEqual(t, time.Since(start), opts.CallTimeout)
		require.True(t, out.Pending)
		require.Equal(t, "unresolved", out.Operation.State)
		select {
		case <-exited:
		default:
			t.Fatal("gateway still active after operation returned")
		}
	})
}
func TestSTR005RecoveryRules(t *testing.T) { // STR-005
	cases := []struct {
		name      string
		attempts  int
		elapsed   time.Duration
		wantCalls int
	}{
		{"one_attempt_create_budget", 1, 0, 1},                       // STR-005
		{"two_attempt_create_budget", 2, 0, 2},                       // STR-005
		{"three_attempt_create_budget", 3, 0, 3},                     // STR-005
		{"slow_call_exhausts_elapsed_budget", 3, 7 * time.Second, 1}, // STR-005
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			reply := policyReply{run: func(_ context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
				c.Advance(tc.elapsed)
				return SessionEvidence{ErrorClass: "transport"}, errors.New("timeout")
			}}
			g := newPolicyGateway(reply, reply, reply)
			opts := policyOptions(c)
			opts.MaxAttempts = tc.attempts
			s := policyService(t, r, g, opts)
			out, err := s.Create(policyCtx(), policyInput())
			require.NoError(t, err)
			policyAssertUnpaid(t, out)
			require.True(t, out.Pending)
			require.Equal(t, "unresolved", out.Operation.State)
			calls := g.Calls()
			require.Len(t, calls, tc.wantCalls)
			for _, call := range calls {
				require.Equal(t, "POST", call.Method)
				require.Equal(t, policyImmutableSnapshot(calls[0].Snapshot), policyImmutableSnapshot(call.Snapshot))
				require.Equal(t, out.Operation.ID, call.Snapshot.OperationID)
			}
			require.Len(t, r.State().Operations, 1)
		})
	}
	t.Run("retrieval_and_creation_share_attempt_counter", func(t *testing.T) { // STR-005
		c := newPolicyClock()
		v := policyFixture(c, "expired")
		r := newPolicyRepository(v)
		e := policyEvidence(v.Operation.Snapshot)
		e.Status = "expired"
		g := newPolicyGateway(policyReply{evidence: e}, policyReply{evidence: SessionEvidence{ErrorClass: "transport"}, err: errors.New("response lost")}, policyOpenReply())
		opts := policyOptions(c)
		opts.MaxAttempts = 2
		s := policyService(t, r, g, opts)
		out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		require.NoError(t, err)
		require.True(t, out.Pending)
		require.Equal(t, "unresolved", out.Operation.State)
		calls := g.Calls()
		require.Len(t, calls, 2)
		require.Equal(t, "GET", calls[0].Method)
		require.Equal(t, "POST", calls[1].Method)
		require.Equal(t, out.Operation.StripeKey, calls[1].Snapshot.StripeKey)
		require.Len(t, r.State().Operations, 2)
	})
	t.Run("remaining_elapsed_time_caps_last_call", func(t *testing.T) { // STR-005 STR-004
		c := newPolicyClock()
		r := newPolicyRepository()
		var remaining time.Duration
		g := newPolicyGateway(policyReply{run: func(_ context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
			c.Advance(6 * time.Second)
			return SessionEvidence{ErrorClass: "transport"}, errors.New("transient")
		}}, policyReply{run: func(ctx context.Context, s Snapshot, _ string) (SessionEvidence, error) {
			end, ok := ctx.Deadline()
			require.True(t, ok)
			remaining = time.Until(end)
			return policyEvidence(s), nil
		}})
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		require.True(t, out.Established)
		require.Len(t, g.Calls(), 2)
		require.Greater(t, remaining, time.Duration(0))
		require.LessOrEqual(t, remaining, 750*time.Millisecond)
	})
}
func TestSTR006RecoveryRules(t *testing.T) { // STR-006
	cases := []struct {
		name        string
		header      time.Duration
		shouldRetry *bool
		budget      time.Duration
		calls       int
		waits       []time.Duration
	}{
		{"default_second_and_third_minimums", 0, nil, 7 * time.Second, 3, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}},                          // STR-006
		{"short_retry_after_uses_policy_minimum", 100 * time.Millisecond, nil, 7 * time.Second, 3, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}}, // STR-006
		{"long_retry_after_sets_minimum", time.Second, nil, 7 * time.Second, 3, []time.Duration{time.Second, time.Second}},                                          // STR-006
		{"required_wait_cannot_fit", time.Second, nil, 500 * time.Millisecond, 1, nil},                                                                              // STR-006
		{"stripe_should_retry_false", 0, policyPtr(false), 7 * time.Second, 1, nil},                                                                                 // STR-006
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			reply := policyReply{evidence: SessionEvidence{ErrorClass: "rate_limit", RetryAfter: tc.header, StripeShouldRetry: tc.shouldRetry}, err: errors.New("rate limited")}
			g := newPolicyGateway(reply, reply, reply)
			opts := policyOptions(c)
			opts.RetryBudget = tc.budget
			if opts.CallTimeout > tc.budget {
				opts.CallTimeout = tc.budget
			}
			s := policyService(t, r, g, opts)
			out, err := s.Create(policyCtx(), policyInput())
			require.NoError(t, err)
			require.True(t, out.Pending)
			require.Len(t, g.Calls(), tc.calls)
			require.Equal(t, tc.waits, c.Waits())
			require.Equal(t, "unresolved", out.Operation.State)
		})
	}
	t.Run("cancellation_during_wait_prevents_later_call", func(t *testing.T) { // STR-006 STR-004
		c := newPolicyClock()
		ctx, cancel := context.WithCancel(policyCtx())
		defer cancel()
		c.waitHook = func(callctx context.Context, _ time.Duration) error { cancel(); <-callctx.Done(); return callctx.Err() }
		r := newPolicyRepository()
		g := newPolicyGateway(policyReply{evidence: SessionEvidence{ErrorClass: "transport"}, err: errors.New("reset")}, policyOpenReply())
		s := policyService(t, r, g, policyOptions(c))
		_, _ = s.Create(ctx, policyInput())
		require.Len(t, g.Calls(), 1)
		require.Equal(t, []time.Duration{250 * time.Millisecond}, c.Waits())
		require.Len(t, r.State().Operations, 1)
		for _, op := range r.State().Operations {
			require.Equal(t, "unresolved", op.State)
		}
	})
	t.Run("wait_deadline_error_prevents_later_call", func(t *testing.T) { // STR-006
		c := newPolicyClock()
		c.waitHook = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
		r := newPolicyRepository()
		g := newPolicyGateway(policyReply{evidence: SessionEvidence{ErrorClass: "transport"}, err: errors.New("reset")}, policyOpenReply())
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		require.True(t, out.Pending)
		require.Len(t, g.Calls(), 1)
		require.Equal(t, "unresolved", out.Operation.State)
	})
}
func TestSTR007RecoveryRules(t *testing.T) { // STR-007
	cases := []struct {
		name  string
		class string
		cause error
	}{
		{"transport_timeout", "transport", context.DeadlineExceeded},                              // STR-007
		{"connection_reset_response_loss", "transport", errors.New("connection reset")},           // STR-007
		{"rate_limit_429", "rate_limit", errors.New("rate limit")},                                // STR-007
		{"documented_transient_conflict", "transient_conflict", errors.New("concurrent request")}, // STR-007
		{"indeterminate_server_5xx", "server", errors.New("server failure")},                      // STR-007
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			g := newPolicyGateway(policyReply{evidence: SessionEvidence{ErrorClass: tc.class}, err: tc.cause}, policyOpenReply())
			s := policyService(t, r, g, policyOptions(c))
			out, err := s.Create(policyCtx(), policyInput())
			require.NoError(t, err)
			policyAssertUnpaid(t, out)
			require.Equal(t, "open", out.Operation.State)
			require.True(t, out.Established)
			calls := g.Calls()
			require.Len(t, calls, 2)
			require.Equal(t, policyImmutableSnapshot(calls[0].Snapshot), policyImmutableSnapshot(calls[1].Snapshot))
			require.Equal(t, out.Operation.StripeKey, calls[1].Snapshot.StripeKey)
			require.NotEqual(t, policyInput().RequestKey, out.Operation.StripeKey)
			require.Len(t, r.State().Operations, 1)
		})
	}
	t.Run("cached_repeated_5xx_stays_indeterminate", func(t *testing.T) { // STR-007 REC-004
		c := newPolicyClock()
		r := newPolicyRepository()
		reply := policyReply{evidence: SessionEvidence{ErrorClass: "server"}, err: errors.New("cached failure")}
		g := newPolicyGateway(reply, reply, reply)
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		require.True(t, out.Pending)
		require.True(t, out.NeedsInvestigation)
		require.Equal(t, "unresolved", out.Operation.State)
		require.False(t, out.CanStartNewAttempt)
		require.Len(t, g.Calls(), 3)
		for _, call := range g.Calls() {
			require.Equal(t, policyImmutableSnapshot(g.Calls()[0].Snapshot), policyImmutableSnapshot(call.Snapshot))
		}
	})
	t.Run("safe_age_exhausted_during_backoff_prevents_second_creation", func(t *testing.T) { // STR-007 REC-003
		c := newPolicyClock()
		v := policyFixture(c, "unresolved")
		v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-23*time.Hour + 100*time.Millisecond))
		v.Operation.Snapshot.FirstDispatchAt = clonePolicyPtr(v.Operation.FirstDispatchAt)
		g := newPolicyGateway(policyReply{evidence: SessionEvidence{ErrorClass: "transport"}, err: errors.New("response lost")}, policyOpenReply())
		r, s := policyContinue(t, v, g, c)
		out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		require.NoError(t, err)
		require.True(t, out.Pending)
		require.True(t, out.NeedsInvestigation)
		require.Len(t, g.Calls(), 1)
		require.Len(t, r.State().Operations, 1)
		require.Equal(t, v.Operation.ID, out.Operation.ID)
	})
}
func TestSTR008RecoveryRules(t *testing.T) { // STR-008
	cases := []struct {
		name     string
		class    string
		prior    bool
		rejected bool
	}{
		{"first_structured_validation", "validation", false, true},         // STR-008
		{"first_sandbox_credential", "credential", false, true},            // STR-008
		{"first_sandbox_permission", "permission", false, true},            // STR-008
		{"validation_after_response_loss", "validation", true, false},      // STR-008
		{"credential_after_interruption", "credential", true, false},       // STR-008
		{"permission_after_uncertain_dispatch", "permission", true, false}, // STR-008
		{"generic_4xx_is_uncertain", "generic", false, false},              // STR-008
		{"idempotency_mismatch_is_uncertain", "idempotency", false, false}, // STR-008
		{"uncertain_dependency_error", "", false, false},                   // STR-008
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			if tc.prior {
				v := policyFixture(c, "unresolved")
				v.Operation.PriorAmbiguity = true
				r = newPolicyRepository(v)
				in := policyInput()
				r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
			}
			g := newPolicyGateway(policyReply{evidence: SessionEvidence{ErrorClass: tc.class}, err: errors.New("prepared rejection")})
			s := policyService(t, r, g, policyOptions(c))
			out, err := s.Create(policyCtx(), policyInput())
			if tc.rejected {
				policyErrorCode(t, err, "checkout_rejected")
				require.True(t, out.ConfirmedRejected)
				require.Equal(t, "rejected", out.Operation.State)
				require.True(t, out.CanStartNewAttempt)
				before := r.State()
				again, err := s.Create(policyCtx(), policyInput())
				policyErrorCode(t, err, "checkout_rejected")
				require.Equal(t, out.Operation.ID, again.Operation.ID)
				require.Equal(t, before, r.State())
			} else {
				require.NoError(t, err)
				require.True(t, out.Pending)
				require.Equal(t, "unresolved", out.Operation.State)
				require.False(t, out.CanStartNewAttempt)
			}
			require.True(t, out.NeedsInvestigation)
			policyAssertUnpaid(t, out)
			require.Len(t, g.Calls(), 1)
		})
	}
}
func TestREC003RecoveryRules(t *testing.T) { // REC-003
	cases := []struct {
		name    string
		age     time.Duration
		fresh   bool
		known   bool
		allowed bool
	}{
		{"original_key_just_before_cutoff", 23*time.Hour - time.Nanosecond, false, false, true},      // REC-003
		{"original_key_exact_cutoff_restart", 23 * time.Hour, false, false, false},                   // REC-003
		{"original_key_after_cutoff", 23*time.Hour + time.Nanosecond, false, false, false},           // REC-003
		{"fresh_key_just_before_cutoff", 23*time.Hour - time.Nanosecond, true, false, true},          // REC-003
		{"fresh_key_exact_cutoff_restart", 23 * time.Hour, true, false, false},                       // REC-003
		{"fresh_key_after_cutoff", 23*time.Hour + time.Nanosecond, true, false, false},               // REC-003
		{"known_id_old_original_key_retrieves", 23*time.Hour + 30*time.Minute, false, true, true},    // REC-003
		{"known_id_old_fresh_key_retrieves", 23*time.Hour + 30*time.Minute, true, true, true},        // REC-003
		{"session_expiry_cannot_extend_safe_age", 23*time.Hour + 30*time.Minute, true, false, false}, // REC-003
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "unresolved")
			first := c.Now().Add(-tc.age)
			v.Operation.FirstDispatchAt = &first
			v.Operation.Snapshot.FirstDispatchAt = &first
			v.Operation.Snapshot.ExpiresAt = first.Add(23*time.Hour + 59*time.Minute).Unix()
			v.Operation.PriorAmbiguity = true
			if tc.known {
				v.Operation.SessionID = policyPtr("cs_test_policy")
			}
			r := newPolicyRepository(v)
			in := policyInput()
			r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
			g := newPolicyGateway(policyOpenReply())
			if tc.known {
				e := policyEvidence(v.Operation.Snapshot)
				e.ExpiresAt = c.Now().Add(time.Hour).Unix()
				g = newPolicyGateway(policyReply{evidence: e})
			}
			s := policyService(t, r, g, policyOptions(c))
			var out Outcome
			var err error
			if tc.fresh {
				out, err = s.Continue(policyCtx(), v.Order.ID, policyKey())
			} else {
				out, err = s.Create(policyCtx(), in)
			}
			if tc.allowed {
				require.NoError(t, err)
				require.Len(t, g.Calls(), 1)
				want := "POST"
				if tc.known {
					want = "GET"
				}
				require.Equal(t, want, g.Calls()[0].Method)
				require.Equal(t, v.Operation.ID, out.Operation.ID)
			} else {
				if tc.fresh {
					policyErrorCode(t, err, "checkout_blocked")
					require.NotContains(t, r.State().Bindings, policyKey())
				} else {
					require.NoError(t, err)
					require.True(t, out.Pending)
					require.True(t, out.NeedsInvestigation)
				}
				require.Empty(t, g.Calls())
				require.Equal(t, "unresolved", r.State().Operations[v.Operation.ID].State)
			}
			require.Len(t, r.State().Operations, 1)
			require.Equal(t, v.Order.Amount, r.State().Orders[v.Order.ID].Amount)
		})
	}
	t.Run("old_known_id_mismatched_read_cannot_establish_or_replace", func(t *testing.T) { // REC-003
		c := newPolicyClock()
		v := policyFixture(c, "unresolved")
		v.Operation.SessionID = policyPtr("cs_test_policy")
		v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-48 * time.Hour))
		e := policyEvidence(v.Operation.Snapshot)
		e.Status = "expired"
		e.Metadata["operation_id"] = "wrong-operation"
		g := newPolicyGateway(policyReply{evidence: e})
		r, s := policyContinue(t, v, g, c)
		_, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		policyErrorCode(t, err, "checkout_blocked")
		require.Len(t, g.Calls(), 1)
		require.Equal(t, "GET", g.Calls()[0].Method)
		require.Len(t, r.State().Operations, 1)
		require.Equal(t, "unresolved", r.State().Operations[v.Operation.ID].State)
	})
}
func TestREC004RecoveryRules(t *testing.T) { // REC-004
	cases := []struct {
		name         string
		state        string
		age          time.Duration
		failure      string
		incompatible bool
		want         bool
	}{
		{"unresolved_just_before_fifteen_minutes", "unresolved", 15*time.Minute - time.Nanosecond, "", false, false}, // REC-004
		{"unresolved_at_fifteen_minutes", "unresolved", 15 * time.Minute, "", false, true},                           // REC-004
		{"unresolved_after_fifteen_minutes", "unresolved", 15*time.Minute + time.Nanosecond, "", false, true},        // REC-004
		{"young_network_ambiguity", "unresolved", time.Minute, "transport", false, false},                            // REC-004
		{"young_mismatched_evidence", "unresolved", time.Minute, "evidence_mismatch", false, true},                   // REC-004
		{"young_indeterminate_5xx", "unresolved", time.Minute, "server", false, true},                                // REC-004
		{"young_incompatible_snapshot", "unresolved", time.Minute, "", true, true},                                   // REC-004
		{"confirmed_configuration_rejection", "rejected", time.Minute, "credential", false, true},                    // REC-004
		{"confirmed_integration_rejection", "rejected", time.Minute, "validation", false, true},                      // REC-004
		{"safe_retry_age_exhausted", "unresolved", 23 * time.Hour, "", false, true},                                  // REC-004
		{"prepared_without_dispatch_has_no_age_trigger", "prepared", 0, "", false, false},                            // REC-004
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, tc.state)
			if tc.state != "prepared" {
				v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-tc.age))
			}
			if tc.failure != "" {
				v.Operation.FailureCode = policyPtr(tc.failure)
			}
			if tc.incompatible {
				v.Operation.Snapshot.APIVersion = "unsupported-version"
			}
			r := newPolicyRepository(v)
			g := newPolicyGateway()
			s := policyService(t, r, g, policyOptions(c))
			before := r.State()
			out, err := s.Get(policyCtx(), v.Order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, out.NeedsInvestigation)
			require.Equal(t, "unpaid", out.Order.Status)
			require.Equal(t, tc.state, out.Operation.State)
			policyAssertNoEffects(t, r, g, before)
		})
	}
	t.Run("investigation_advisory_still_allows_safe_recovery", func(t *testing.T) { // REC-004
		c := newPolicyClock()
		v := policyFixture(c, "unresolved")
		v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-16 * time.Minute))
		g := newPolicyGateway(policyOpenReply())
		r, s := policyContinue(t, v, g, c)
		view, err := s.Get(policyCtx(), v.Order.ID)
		require.NoError(t, err)
		require.True(t, view.NeedsInvestigation)
		require.True(t, view.CanRetrySameOperation)
		out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		require.NoError(t, err)
		require.Equal(t, "open", out.Operation.State)
		require.Equal(t, v.Operation.ID, out.Operation.ID)
		require.False(t, out.NeedsInvestigation)
		require.Len(t, g.Calls(), 1)
		require.Equal(t, "POST", g.Calls()[0].Method)
		require.Len(t, r.State().Operations, 1)
	})
}

// STR-002 STR-003 STR-007
func TestEachReplayDispatchBookkeepingIsDurable(t *testing.T) {
	c := newPolicyClock()
	r := newPolicyRepository()
	var snapshots []Snapshot
	reply := policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) {
		op := r.State().Operations[s.OperationID]
		require.NotNil(t, op.LastDispatchAt)
		require.Equal(t, op.LastDispatchAt, s.LastDispatchAt)
		require.Equal(t, c.Now(), *op.LastDispatchAt, "last possible send is committed before the gateway call")
		if len(snapshots) > 0 {
			require.Equal(t, policyImmutableSnapshot(snapshots[0]), policyImmutableSnapshot(s))
			require.True(t, s.LastDispatchAt.After(*snapshots[len(snapshots)-1].LastDispatchAt))
		}
		snapshots = append(snapshots, clonePolicySnapshot(s))
		return SessionEvidence{ErrorClass: "transport"}, errors.New("response lost")
	}}
	g := newPolicyGateway(reply, reply, reply)
	s := policyService(t, r, g, policyOptions(c))
	out, err := s.Create(policyCtx(), policyInput())
	require.NoError(t, err)
	require.True(t, out.Pending)
	require.Len(t, snapshots, 3)
}

// REC-003 STR-007
func TestSafeReplayAgeAfterDispatchPreparation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		delay   time.Duration
		allowed bool
	}{
		{"still_before_cutoff", 50 * time.Millisecond, true}, // REC-003
		{"exact_cutoff", 100 * time.Millisecond, false},      // REC-003
		{"after_cutoff", 200 * time.Millisecond, false},      // REC-003
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "unresolved")
			first := c.Now().Add(-23*time.Hour + 100*time.Millisecond)
			v.Operation.FirstDispatchAt = &first
			v.Operation.Snapshot.FirstDispatchAt = &first
			v.Operation.Snapshot.ExpiresAt = first.Add(23*time.Hour + 59*time.Minute).Unix()
			r := newPolicyRepository(v)
			in := policyInput()
			r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
			binding := r.State().Bindings[in.RequestKey]
			prepared := false
			r.hook = func(_ context.Context, method string) error {
				if method == "PrepareDispatch" {
					prepared = true
					// The durable dispatch transaction may finish after eligibility was checked.
					c.Advance(tc.delay)
				}
				return nil
			}
			g := newPolicyGateway(policyReply{run: func(_ context.Context, snapshot Snapshot, _ string) (SessionEvidence, error) {
				require.True(t, c.Now().Before(first.Add(23*time.Hour)), "creation must still be safe at gateway entry")
				require.Equal(t, policyImmutableSnapshot(v.Operation.Snapshot), policyImmutableSnapshot(snapshot))
				return policyEvidence(snapshot), nil
			}})
			s := policyService(t, r, g, policyOptions(c))
			out, err := s.Create(policyCtx(), in)
			require.NoError(t, err)
			require.True(t, prepared, "control must reach dispatch preparation inside the safe window")
			require.Equal(t, v.Order.ID, out.Order.ID)
			require.Equal(t, v.Operation.ID, out.Operation.ID)
			st := r.State()
			require.Equal(t, binding, st.Bindings[in.RequestKey])
			require.Len(t, st.Orders, 1)
			require.Len(t, st.Operations, 1)
			require.Equal(t, policyImmutableSnapshot(v.Operation.Snapshot), policyImmutableSnapshot(st.Operations[v.Operation.ID].Snapshot))
			if tc.allowed {
				require.Len(t, g.Calls(), 1)
				require.Equal(t, "POST", g.Calls()[0].Method)
				require.Equal(t, "open", out.Operation.State)
			} else {
				require.Empty(t, g.Calls(), "transaction delay cannot extend creation replay eligibility")
				require.True(t, out.Pending)
				require.True(t, out.NeedsInvestigation)
				require.Equal(t, "unresolved", out.Operation.State)
				require.False(t, out.CanStartNewAttempt)
				// A fresh service can inspect and replay the retained binding without creating.
				fresh := policyService(t, r, g, policyOptions(c))
				view, readErr := fresh.Get(policyCtx(), v.Order.ID)
				require.NoError(t, readErr)
				require.Equal(t, v.Operation.ID, view.Operation.ID)
				require.True(t, view.NeedsInvestigation)
				replayed, replayErr := fresh.Create(policyCtx(), in)
				require.NoError(t, replayErr)
				require.Equal(t, v.Operation.ID, replayed.Operation.ID)
				require.True(t, replayed.Pending)
				_, replacementErr := fresh.Continue(policyCtx(), v.Order.ID, policyKey())
				policyErrorCode(t, replacementErr, "checkout_blocked")
				require.NotContains(t, r.State().Bindings, policyKey())
				require.Len(t, r.State().Operations, 1)
				require.Empty(t, g.Calls())
			}
		})
	}
}
