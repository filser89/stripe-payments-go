package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestID003CheckoutRules(t *testing.T) { // ID-003
	t.Run("distinct_initial_keys_preserve_distinct_intent", func(t *testing.T) { // ID-003
		c := newPolicyClock()
		r := newPolicyRepository()
		g := newPolicyGateway(policyOpenReply(), policyOpenReply())
		s := policyService(t, r, g, policyOptions(c))
		in := policyInput()
		a, err := s.Create(policyCtx(), in)
		require.NoError(t, err)
		in.RequestKey = policyKey()
		b, err := s.Create(policyCtx(), in)
		require.NoError(t, err)
		require.NotEqual(t, a.Order.ID, b.Order.ID)
		require.NotEqual(t, a.Operation.ID, b.Operation.ID)
		require.NotEqual(t, a.Operation.StripeKey, b.Operation.StripeKey)
		require.NotEqual(t, in.RequestKey, b.Operation.StripeKey)
		require.Equal(t, a.Order.Amount, b.Order.Amount)
		require.Equal(t, a.Order.Description, b.Order.Description)
		require.Len(t, r.State().Orders, 2)
	})
	t.Run("same_initial_key_targets_same_operation", func(t *testing.T) { // ID-003
		c := newPolicyClock()
		r := newPolicyRepository()
		g := newPolicyGateway(policyOpenReply(), policyReply{run: func(_ context.Context, _ Snapshot, id string) (SessionEvidence, error) {
			st := r.State()
			for _, op := range st.Operations {
				e := policyEvidence(op.Snapshot)
				e.SessionID = id
				return e, nil
			}
			return SessionEvidence{}, errors.New("missing persisted operation")
		}})
		s := policyService(t, r, g, policyOptions(c))
		a, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		b, err := s.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		require.Equal(t, a.Order.ID, b.Order.ID)
		require.Equal(t, a.Operation.ID, b.Operation.ID)
		require.Equal(t, a.Operation.StripeKey, b.Operation.StripeKey)
		require.Len(t, r.State().Orders, 1)
		require.Len(t, r.State().Operations, 1)
		require.Len(t, g.Calls(), 2)
		require.Equal(t, "GET", g.Calls()[1].Method)
	})
}
func TestLIFE001CheckoutRules(t *testing.T) { // LIFE-001
	cases := []struct {
		name    string
		status  string
		payment string
		class   string
		want    string
	}{
		{"verified_open_unpaid", "open", "unpaid", "", "open"},                      // LIFE-001
		{"verified_complete_unpaid", "complete", "unpaid", "", "complete_unpaid"},   // LIFE-001
		{"verified_expired_unpaid", "expired", "unpaid", "", "expired"},             // LIFE-001
		{"timeout_is_unresolved", "", "", "transport", "unresolved"},                // LIFE-001
		{"definitive_first_validation_rejection", "", "", "validation", "rejected"}, // LIFE-001 STR-008
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			r := newPolicyRepository()
			g := newPolicyGateway(policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) {
				st := r.State()
				op := st.Operations[s.OperationID]
				require.Equal(t, "unresolved", op.State, "dispatch ambiguity must commit before external call")
				require.NotNil(t, op.FirstDispatchAt)
				require.NotEmpty(t, op.OwnerToken)
				require.Equal(t, policyImmutableSnapshot(op.Snapshot), policyImmutableSnapshot(s))
				require.Equal(t, op.StripeKey, s.StripeKey)
				require.Equal(t, op.FirstDispatchAt, s.FirstDispatchAt)
				require.Equal(t, op.FirstDispatchAt.Add(23*time.Hour+59*time.Minute).Unix(), s.ExpiresAt)
				require.NotEmpty(t, st.Bindings[policyInput().RequestKey].OperationID)
				require.NotEmpty(t, st.History[s.OrderID])
				e := policyEvidence(s)
				e.Status = tc.status
				e.PaymentStatus = tc.payment
				e.ErrorClass = tc.class
				if tc.class != "" {
					return e, errors.New("prepared dependency error")
				}
				return e, nil
			}})
			opts := policyOptions(c)
			opts.MaxAttempts = 1
			s := policyService(t, r, g, opts)
			out, err := s.Create(policyCtx(), policyInput())
			if tc.want == "rejected" {
				policyErrorCode(t, err, "checkout_rejected")
			} else {
				require.NoError(t, err)
			}
			policyAssertUnpaid(t, out)
			require.Equal(t, tc.want, out.Operation.State)
			require.Equal(t, tc.want, r.State().Operations[out.Operation.ID].State)
			if tc.want == "unresolved" {
				require.True(t, out.Pending)
			}
		})
	}
	t.Run("prepared_intent_without_dispatch_is_recoverable", func(t *testing.T) { // LIFE-001
		c := newPolicyClock()
		v := policyFixture(c, "prepared")
		r := newPolicyRepository(v)
		g := newPolicyGateway()
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Get(policyCtx(), v.Order.ID)
		require.NoError(t, err)
		require.Equal(t, "prepared", out.Operation.State)
		require.Nil(t, out.Operation.FirstDispatchAt)
		require.True(t, out.CanRetrySameOperation)
		require.Empty(t, g.Calls())
	})
	t.Run("precall_interruption_remains_unresolved_without_wire_send", func(t *testing.T) { // LIFE-001
		c := newPolicyClock()
		v := policyFixture(c, "unresolved")
		v.Operation.PriorAmbiguity = true
		r := newPolicyRepository(v)
		g := newPolicyGateway()
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Get(policyCtx(), v.Order.ID)
		require.NoError(t, err)
		require.Equal(t, "unresolved", out.Operation.State)
		require.Equal(t, "unpaid", out.Order.Status)
		require.True(t, out.CanRetrySameOperation)
		require.Empty(t, g.Calls())
	})
}
func TestLIFE002CheckoutRules(t *testing.T) { // LIFE-002
	cases := []struct {
		name      string
		status    string
		payment   string
		failure   bool
		mismatch  bool
		age       time.Duration
		wantState string
		blocked   bool
	}{
		{"open_refresh_resumes_same_page", "open", "unpaid", false, false, time.Minute, "open", false},                            // LIFE-002
		{"locally_expired_still_open_refresh", "open", "unpaid", false, false, 24 * time.Hour, "open", false},                     // LIFE-002
		{"known_id_beyond_safe_age_uses_retrieval", "open", "unpaid", false, false, 23*time.Hour + 30*time.Minute, "open", false}, // LIFE-002 REC-003
		{"decline_cancel_abandon_leave_open", "open", "unpaid", false, false, time.Minute, "open", false},                         // LIFE-002 LIFE-005
		{"complete_unpaid_cannot_resume", "complete", "unpaid", false, false, time.Minute, "complete_unpaid", true},               // LIFE-002
		{"paid_evidence_awaits_confirmation", "complete", "paid", false, false, time.Minute, "unresolved", true},                  // LIFE-002 LIFE-007
		{"retrieval_failure_suppresses_saved_url", "open", "unpaid", true, false, time.Minute, "unresolved", true},                // LIFE-002
		{"retrieval_mismatch_suppresses_saved_url", "open", "unpaid", false, true, time.Minute, "unresolved", true},               // LIFE-002
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "open")
			first := c.Now().Add(-tc.age)
			v.Operation.FirstDispatchAt = &first
			if tc.age >= 24*time.Hour {
				v.Operation.ExpiresAt = policyPtr(c.Now().Add(-time.Minute))
			}
			e := policyEvidence(v.Operation.Snapshot)
			e.Status = tc.status
			e.PaymentStatus = tc.payment
			e.ExpiresAt = c.Now().Add(time.Hour).Unix()
			if tc.payment == "paid" {
				e.PaymentIntentID = policyPtr("pi_policy")
			}
			if tc.mismatch {
				e.AmountTotal++
			}
			reply := policyReply{evidence: e}
			if tc.failure {
				reply.err = errors.New("retrieval failed")
				reply.evidence.ErrorClass = "transport"
			}
			g := newPolicyGateway(reply)
			r := newPolicyRepository(v)
			opts := policyOptions(c)
			opts.MaxAttempts = 1
			s := policyService(t, r, g, opts)
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			if tc.blocked {
				policyErrorCode(t, err, "checkout_blocked")
				require.NotContains(t, r.State().Bindings, policyKey())
			} else {
				require.NoError(t, err)
				require.True(t, out.CanResume)
				require.Equal(t, v.Operation.SessionID, out.Operation.SessionID)
				require.Equal(t, e.URL, *out.Operation.CheckoutURL)
			}
			require.Len(t, g.Calls(), 1)
			require.Equal(t, "GET", g.Calls()[0].Method)
			require.Equal(t, *v.Operation.SessionID, g.Calls()[0].SessionID)
			require.Len(t, r.State().Operations, 1)
			require.Equal(t, "unpaid", r.State().Orders[v.Order.ID].Status)
			if tc.blocked {
				require.False(t, out.CanResume)
			}
			require.Equal(t, tc.wantState, r.State().Operations[v.Operation.ID].State)
		})
	}
}
func TestLIFE003CheckoutRules(t *testing.T) { // LIFE-003
	cases := []struct {
		name      string
		state     string
		known     bool
		active    bool
		ambiguous bool
	}{
		{"prepared_dispatch_binds_same_operation", "prepared", false, false, false},   // LIFE-003
		{"unresolved_missing_id_safe_replay", "unresolved", false, false, false},      // LIFE-003
		{"unresolved_known_id_retrieval", "unresolved", true, false, false},           // LIFE-003
		{"active_dispatch_returns_pending_binding", "unresolved", false, true, false}, // LIFE-003
		{"accepted_response_loss_retains_binding", "unresolved", false, false, true},  // LIFE-003
		{"open_refresh_binds_same_identity", "open", true, false, false},              // LIFE-003
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, tc.state)
			if tc.known {
				v.Operation.SessionID = policyPtr("cs_test_policy")
			}
			if tc.active {
				v.Operation.OwnerToken = "other-dispatch-owner"
			}
			r := newPolicyRepository(v)
			e := policyEvidence(v.Operation.Snapshot)
			reply := policyReply{evidence: e}
			if !tc.known {
				reply = policyOpenReply()
			}
			if tc.ambiguous {
				reply = policyReply{evidence: SessionEvidence{ErrorClass: "transport"}, err: errors.New("response lost")}
			}
			g := newPolicyGateway(reply)
			opts := policyOptions(c)
			opts.MaxAttempts = 1
			s := policyService(t, r, g, opts)
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			require.NoError(t, err)
			require.Equal(t, v.Order.ID, out.Order.ID)
			require.Equal(t, v.Operation.ID, out.Operation.ID)
			require.Equal(t, v.Operation.StripeKey, out.Operation.StripeKey)
			require.Len(t, r.State().Operations, 1)
			require.Equal(t, v.Operation.ID, r.State().Bindings[policyKey()].OperationID)
			if tc.active || tc.ambiguous {
				require.True(t, out.Pending)
				require.Equal(t, "unresolved", out.Operation.State)
			}
			if tc.active {
				require.Empty(t, g.Calls())
			} else {
				require.Len(t, g.Calls(), 1)
				if tc.known {
					require.Equal(t, "GET", g.Calls()[0].Method)
				} else {
					require.Equal(t, "POST", g.Calls()[0].Method)
					require.Equal(t, v.Operation.StripeKey, g.Calls()[0].Snapshot.StripeKey)
				}
			}
		})
	}
	t.Run("accepted_mismatch_keeps_key_and_operation_on_replay", func(t *testing.T) { // LIFE-003 LIFE-005
		c := newPolicyClock()
		v := policyFixture(c, "prepared")
		r := newPolicyRepository(v)
		g := newPolicyGateway(policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) {
			e := policyEvidence(s)
			e.AmountTotal++
			return e, nil
		}})
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		require.NoError(t, err)
		require.True(t, out.Pending)
		require.Equal(t, "unresolved", out.Operation.State)
		require.Equal(t, v.Operation.ID, r.State().Bindings[policyKey()].OperationID)
		require.True(t, out.NeedsInvestigation)
		require.False(t, out.CanStartNewAttempt)
		require.Len(t, r.State().Operations, 1)
	})
}
func TestLIFE004CheckoutRules(t *testing.T) { // LIFE-004
	cases := []struct {
		name        string
		state       string
		paidRace    bool
		currentRace bool
	}{
		{"verified_expired_allows_distinct_attempt", "expired", false, false},   // LIFE-004 ID-003
		{"confirmed_rejection_needs_no_retrieval", "rejected", false, false},    // LIFE-004 ID-003
		{"paid_guard_wins_after_evidence", "expired", true, false},              // LIFE-004 LIFE-006
		{"current_operation_guard_wins_after_evidence", "expired", false, true}, // LIFE-004
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, tc.state)
			r := newPolicyRepository(v)
			e := policyEvidence(v.Operation.Snapshot)
			e.Status = "expired"
			e.PaymentStatus = "unpaid"
			g := newPolicyGateway(policyReply{evidence: e}, policyOpenReply())
			if tc.state == "rejected" {
				g = newPolicyGateway(policyOpenReply())
			}
			if tc.paidRace || tc.currentRace {
				r.beforeBind = func(st *policyRepoState) {
					o := st.Orders[v.Order.ID]
					if tc.paidRace {
						o.Status = "paid"
					} else {
						o.CurrentOperationID = "different-current-operation"
						op := v.Operation
						op.ID = o.CurrentOperationID
						op.State = "open"
						st.Operations[op.ID] = op
					}
					st.Orders[o.ID] = o
				}
			}
			s := policyService(t, r, g, policyOptions(c))
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			if tc.paidRace || tc.currentRace {
				require.Error(t, err)
				require.NotContains(t, r.State().Bindings, policyKey())
				require.Len(t, g.Calls(), 1)
				require.Equal(t, "GET", g.Calls()[0].Method)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, v.Operation.ID, out.Operation.ID)
				require.NotEqual(t, v.Operation.StripeKey, out.Operation.StripeKey)
				require.Equal(t, v.Order.ID, out.Order.ID)
				require.Equal(t, v.Order.Amount, out.Order.Amount)
				require.Equal(t, v.Order.Currency, out.Order.Currency)
				require.Equal(t, v.Order.Description, out.Order.Description)
				require.Len(t, r.State().Operations, 2)
				require.Equal(t, out.Operation.ID, r.State().Bindings[policyKey()].OperationID)
				calls := g.Calls()
				require.Equal(t, "POST", calls[len(calls)-1].Method)
				if tc.state == "rejected" {
					require.Len(t, calls, 1)
				} else {
					require.Len(t, calls, 2)
					require.Equal(t, "GET", calls[0].Method)
				}
			}
		})
	}
}
func TestLIFE005CheckoutRules(t *testing.T) { // LIFE-005
	cases := []struct {
		name   string
		state  string
		mutate func(*View)
		reply  func(View) policyReply
	}{
		{"complete_unpaid_including_decline", "complete_unpaid", nil, nil}, // LIFE-005
		{"local_paid", "paid", nil, nil},                                   // LIFE-005
		{"old_missing_id_ambiguity", "unresolved", func(v *View) { v.Operation.FirstDispatchAt = policyPtr(v.Order.CreatedAt.Add(-23 * time.Hour)) }, nil}, // LIFE-005 REC-003
		{"incompatible_snapshot", "unresolved", func(v *View) { v.Operation.Snapshot.APIVersion = "unsupported-version" }, nil},                            // LIFE-005 LIFE-003
		{"earlier_ambiguity_blocks_rejected_attempt", "rejected", func(v *View) { v.Operation.PriorAmbiguity = true }, nil},                                // LIFE-005
		{"payment_intent_only", "open", nil, func(v View) policyReply {
			e := policyEvidence(v.Operation.Snapshot)
			e.SessionID = ""
			e.PaymentIntentID = policyPtr("pi_only")
			e.Status = "complete"
			e.PaymentStatus = "paid"
			return policyReply{evidence: e}
		}}, // LIFE-005
		{"no_payment_required", "open", nil, func(v View) policyReply {
			e := policyEvidence(v.Operation.Snapshot)
			e.Status = "complete"
			e.PaymentStatus = "no_payment_required"
			return policyReply{evidence: e}
		}}, // LIFE-005
		{"unknown_status_combination", "open", nil, func(v View) policyReply {
			e := policyEvidence(v.Operation.Snapshot)
			e.Status = "unknown"
			return policyReply{evidence: e}
		}}, // LIFE-005
		{"malformed_retrieval", "open", nil, func(View) policyReply { return policyReply{} }}, // LIFE-005
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, tc.state)
			if tc.mutate != nil {
				tc.mutate(&v)
			}
			r := newPolicyRepository(v)
			g := newPolicyGateway()
			if tc.reply != nil {
				g = newPolicyGateway(tc.reply(v))
			}
			opts := policyOptions(c)
			opts.MaxAttempts = 1
			s := policyService(t, r, g, opts)
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			policyErrorCode(t, err, "checkout_blocked")
			require.False(t, out.CanResume)
			require.False(t, out.CanStartNewAttempt)
			require.NotContains(t, r.State().Bindings, policyKey())
			require.Len(t, r.State().Operations, 1)
			for _, call := range g.Calls() {
				require.Equal(t, "GET", call.Method)
			}
			require.Equal(t, v.Order.Status, r.State().Orders[v.Order.ID].Status)
		})
	}
}
func TestLIFE006CheckoutRules(t *testing.T) { // LIFE-006
	t.Run("paid_blocks_fresh_and_bound_continuation", func(t *testing.T) { // LIFE-006 LIFE-001
		c := newPolicyClock()
		v := policyFixture(c, "paid")
		r := newPolicyRepository(v)
		r.state.Bindings[policyKey()] = RequestBinding{Key: policyKey(), Method: "POST", Target: "/api/orders/" + v.Order.ID + "/checkout", OrderID: v.Order.ID, OperationID: v.Operation.ID}
		g := newPolicyGateway()
		s := policyService(t, r, g, policyOptions(c))
		before := r.State()
		_, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
		policyErrorCode(t, err, "checkout_blocked")
		_, err = s.Continue(policyCtx(), v.Order.ID, policyInput().RequestKey)
		policyErrorCode(t, err, "checkout_blocked")
		policyAssertNoEffects(t, r, g, before)
	})
	t.Run("initial_replay_paid_historical_open_suppresses_all_actions", func(t *testing.T) { // LIFE-006 LIFE-008
		c := newPolicyClock()
		v := policyFixture(c, "open")
		current := policyFixture(c, "paid")
		current.Operation.ID = "f3b1118d-bc32-4d4c-9efe-305fb41bcda9"
		current.Operation.Snapshot.OperationID = current.Operation.ID
		current.Order.CurrentOperationID = current.Operation.ID
		v.Order = current.Order
		r := newPolicyRepository(v, current)
		in := policyInput()
		r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
		g := newPolicyGateway()
		s := policyService(t, r, g, policyOptions(c))
		before := r.State()
		out, err := s.Create(policyCtx(), in)
		require.NoError(t, err)
		require.Equal(t, "paid", out.Order.Status)
		require.Equal(t, v.Operation.ID, out.Operation.ID)
		require.Nil(t, out.Operation.CheckoutURL)
		require.False(t, out.CanResume)
		require.False(t, out.CanRetrySameOperation)
		require.False(t, out.CanStartNewAttempt)
		policyAssertNoEffects(t, r, g, before)
	})
	cases := []struct {
		name      string
		failure   bool
		retrieval bool
	}{
		{"paid_during_outstanding_creation_success", false, false}, // LIFE-006
		{"paid_during_outstanding_creation_failure", true, false},  // LIFE-006
		{"paid_during_outstanding_retrieval", false, true},         // LIFE-006
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			state := "prepared"
			if tc.retrieval {
				state = "open"
			}
			v := policyFixture(c, state)
			r := newPolicyRepository(v)
			g := newPolicyGateway(policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) {
				r.mu.Lock()
				o := r.state.Orders[v.Order.ID]
				o.Status = "paid"
				r.state.Orders[o.ID] = o
				op := r.state.Operations[v.Operation.ID]
				op.State = "paid"
				r.state.Operations[op.ID] = op
				r.mu.Unlock()
				if tc.failure {
					return SessionEvidence{ErrorClass: "transport"}, errors.New("delayed failure")
				}
				if tc.retrieval {
					s = v.Operation.Snapshot
				}
				return policyEvidence(s), nil
			}})
			opts := policyOptions(c)
			opts.MaxAttempts = 1
			s := policyService(t, r, g, opts)
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			if err == nil {
				require.Equal(t, "paid", out.Order.Status)
				require.Nil(t, out.Operation.CheckoutURL)
				require.False(t, out.CanResume)
				require.False(t, out.CanRetrySameOperation)
				require.False(t, out.CanStartNewAttempt)
			} else {
				var domainErr *Error
				require.ErrorAs(t, err, &domainErr)
				require.Equal(t, v.Order.ID, domainErr.OrderID)
			}
			st := r.State()
			require.Equal(t, "paid", st.Orders[v.Order.ID].Status)
			require.Equal(t, "paid", st.Operations[v.Operation.ID].State)
			require.Len(t, st.Operations, 1)
			require.Len(t, g.Calls(), 1)
			for _, h := range st.History[v.Order.ID] {
				require.NotEqual(t, "unpaid", h.State, "stale result cannot regress the confirmed prerequisite")
			}
		})
	}
}
func TestLIFE007CheckoutRules(t *testing.T) { // LIFE-007
	cases := []struct {
		name   string
		mutate func(*SessionEvidence)
	}{
		{"missing_session", func(e *SessionEvidence) { e.SessionID = "" }},                                        // LIFE-007
		{"wrong_client_reference", func(e *SessionEvidence) { e.ClientReferenceID = "other-order" }},              // LIFE-007
		{"missing_client_reference", func(e *SessionEvidence) { e.ClientReferenceID = "" }},                       // LIFE-007
		{"missing_order_metadata", func(e *SessionEvidence) { delete(e.Metadata, "order_id") }},                   // LIFE-007
		{"wrong_order_metadata", func(e *SessionEvidence) { e.Metadata["order_id"] = "other-order" }},             // LIFE-007
		{"missing_operation_metadata", func(e *SessionEvidence) { delete(e.Metadata, "operation_id") }},           // LIFE-007
		{"wrong_operation_metadata", func(e *SessionEvidence) { e.Metadata["operation_id"] = "other-operation" }}, // LIFE-007
		{"wrong_amount", func(e *SessionEvidence) { e.AmountTotal++ }},                                            // LIFE-007
		{"wrong_currency", func(e *SessionEvidence) { e.Currency = "eur" }},                                       // LIFE-007
		{"wrong_mode", func(e *SessionEvidence) { e.Mode = "subscription" }},                                      // LIFE-007
		{"live_object", func(e *SessionEvidence) { e.Livemode = true }},                                           // LIFE-007
		{"missing_open_url", func(e *SessionEvidence) { e.URL = "" }},                                             // LIFE-007
		{"unknown_state", func(e *SessionEvidence) { e.Status = "unknown" }},                                      // LIFE-007
		{"open_paid_combination", func(e *SessionEvidence) { e.PaymentStatus = "paid" }},                          // LIFE-007
		{"complete_paid_matching_intent_awaits_confirmation", func(e *SessionEvidence) {
			e.Status = "complete"
			e.PaymentStatus = "paid"
			e.PaymentIntentID = policyPtr("pi_policy")
		}}, // LIFE-007
		{"complete_paid_missing_intent", func(e *SessionEvidence) { e.Status = "complete"; e.PaymentStatus = "paid"; e.PaymentIntentID = nil }}, // LIFE-007
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("initial_creation", func(t *testing.T) { // LIFE-007
				policyInvalidEvidence(t, tc.mutate, "initial")
			})
			t.Run("missing_id_creation_replay", func(t *testing.T) { // LIFE-007
				policyInvalidEvidence(t, tc.mutate, "replay")
			})
			t.Run("known_session_retrieval", func(t *testing.T) { // LIFE-007
				policyInvalidEvidence(t, tc.mutate, "retrieve")
			})
		})
	}

	knownCases := []struct {
		name         string
		wrongSession bool
		wrongIntent  bool
	}{
		{"different_known_session_id", true, false},     // LIFE-007
		{"different_known_payment_intent", false, true}, // LIFE-007
	}
	for _, tc := range knownCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "open")
			v.Operation.PaymentIntentID = policyPtr("pi_saved")
			e := policyEvidence(v.Operation.Snapshot)
			e.PaymentIntentID = policyPtr("pi_saved")
			if tc.wrongSession {
				e.SessionID = "cs_other"
			}
			if tc.wrongIntent {
				e.PaymentIntentID = policyPtr("pi_other")
			}
			g := newPolicyGateway(policyReply{evidence: e})
			r, s := policyContinue(t, v, g, c)
			_, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			policyErrorCode(t, err, "checkout_blocked")
			require.Equal(t, v.Operation.SessionID, r.State().Operations[v.Operation.ID].SessionID)
			require.Equal(t, v.Operation.PaymentIntentID, r.State().Operations[v.Operation.ID].PaymentIntentID)
			require.Len(t, g.Calls(), 1)
			require.Equal(t, "GET", g.Calls()[0].Method)
			require.Len(t, r.State().Operations, 1)
		})
	}

}
func TestLIFE008CheckoutRules(t *testing.T) { // LIFE-008
	cases := []struct {
		name   string
		state  string
		mutate func(*View, *policyClock)
		resume bool
		retry  bool
		fresh  bool
	}{
		{"open_with_url_and_future_expiry", "open", nil, true, false, false},                                                                                  // LIFE-008
		{"open_missing_url", "open", func(v *View, _ *policyClock) { v.Operation.CheckoutURL = nil }, false, false, false},                                    // LIFE-008
		{"open_missing_expiry", "open", func(v *View, _ *policyClock) { v.Operation.ExpiresAt = nil }, false, false, false},                                   // LIFE-008
		{"expiry_just_before", "open", func(v *View, c *policyClock) { v.Operation.ExpiresAt = policyPtr(c.Now().Add(time.Nanosecond)) }, true, false, false}, // LIFE-008
		{"expiry_at", "open", func(v *View, c *policyClock) { v.Operation.ExpiresAt = policyPtr(c.Now()) }, false, false, false},                              // LIFE-008
		{"expiry_after", "open", func(v *View, c *policyClock) { v.Operation.ExpiresAt = policyPtr(c.Now().Add(-time.Nanosecond)) }, false, false, false},     // LIFE-008
		{"prepared_same_operation", "prepared", nil, false, true, false},
		{"prepared_unsupported_snapshot", "prepared", func(v *View, _ *policyClock) { v.Operation.Snapshot.APIVersion = "unsupported" }, false, false, false}, // LIFE-008
		{"unresolved_unsupported_api", "unresolved", func(v *View, _ *policyClock) { v.Operation.Snapshot.APIVersion = "unsupported" }, false, false, false},  // LIFE-008
		{"unresolved_known_id_unsupported_snapshot", "unresolved", func(v *View, _ *policyClock) {
			v.Operation.SessionID = policyPtr("cs_test_policy")
			v.Operation.Snapshot.SDKVersion = "unsupported"
		}, false, false, false}, // LIFE-008
		// LIFE-008
		{"unresolved_before_safe_cutoff", "unresolved", func(v *View, c *policyClock) {
			v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-23*time.Hour + time.Nanosecond))
		}, false, true, false}, // LIFE-008
		{"unresolved_at_safe_cutoff", "unresolved", func(v *View, c *policyClock) { v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-23 * time.Hour)) }, false, false, false}, // LIFE-008
		{"unresolved_after_safe_cutoff", "unresolved", func(v *View, c *policyClock) {
			v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-23*time.Hour - time.Nanosecond))
		}, false, false, false}, // LIFE-008
		{"old_unresolved_known_id", "unresolved", func(v *View, c *policyClock) {
			v.Operation.FirstDispatchAt = policyPtr(c.Now().Add(-72 * time.Hour))
			v.Operation.SessionID = policyPtr("cs_test_policy")
		}, false, true, false}, // LIFE-008
		{"unresolved_missing_first_dispatch", "unresolved", func(v *View, _ *policyClock) { v.Operation.FirstDispatchAt = nil }, false, false, false},             // LIFE-008
		{"unresolved_unsupported_snapshot", "unresolved", func(v *View, _ *policyClock) { v.Operation.Snapshot.SDKVersion = "unsupported" }, false, false, false}, // LIFE-008
		{"unresolved_mismatched_evidence", "unresolved", func(v *View, _ *policyClock) {
			v.Operation.FailureCode = policyPtr("evidence_mismatch")
			v.Operation.EvidenceSource = "mismatched"
			v.Operation.InvestigationRequired = true
		}, false, false, false}, // LIFE-008
		{"complete_unpaid_has_no_action", "complete_unpaid", nil, false, false, false},                                                                  // LIFE-008
		{"confirmed_rejected_allows_fresh", "rejected", nil, false, false, true},                                                                        // LIFE-008
		{"rejected_prior_ambiguity_blocks_fresh", "rejected", func(v *View, _ *policyClock) { v.Operation.PriorAmbiguity = true }, false, false, false}, // LIFE-008
		{"verified_expired_allows_fresh", "expired", nil, false, false, true},                                                                           // LIFE-008
		{"paid_all_false", "paid", nil, false, false, false},                                                                                            // LIFE-008
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, tc.state)
			if tc.state == "expired" {
				v.Operation.EvidenceSource = "stripe"
			}
			if tc.mutate != nil {
				tc.mutate(&v, c)
			}
			r := newPolicyRepository(v)
			g := newPolicyGateway()
			s := policyService(t, r, g, policyOptions(c))
			before := r.State()
			out, err := s.Get(policyCtx(), v.Order.ID)
			require.NoError(t, err)
			require.Equal(t, v.Operation.ID, out.Operation.ID)
			require.Equal(t, tc.resume, out.CanResume)
			require.Equal(t, tc.retry, out.CanRetrySameOperation)
			require.Equal(t, tc.fresh, out.CanStartNewAttempt)
			if !tc.resume {
				require.Nil(t, out.Operation.CheckoutURL)
			}
			policyAssertNoEffects(t, r, g, before)
		})
	}
	t.Run("historical_bound_replay_differs_from_current_order_read", func(t *testing.T) { // LIFE-008 ID-004
		c := newPolicyClock()
		old := policyFixture(c, "expired")
		old.Operation.EvidenceSource = "stripe"
		current := policyFixture(c, "prepared")
		current.Operation.ID = "f3b1118d-bc32-4d4c-9efe-305fb41bcda9"
		current.Operation.Snapshot.OperationID = current.Operation.ID
		current.Operation.StripeKey = "new-operation-key"
		current.Operation.Snapshot.StripeKey = current.Operation.StripeKey
		current.Order.CurrentOperationID = current.Operation.ID
		old.Order = current.Order
		r := newPolicyRepository(old, current)
		in := policyInput()
		r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: old.Order.ID, OperationID: old.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
		e := policyEvidence(old.Operation.Snapshot)
		e.Status = "expired"
		g := newPolicyGateway(policyReply{evidence: e})
		s := policyService(t, r, g, policyOptions(c))
		out, err := s.Create(policyCtx(), in)
		require.NoError(t, err)
		require.Equal(t, old.Operation.ID, out.Operation.ID)
		require.False(t, out.CanStartNewAttempt)
		view, err := s.Get(policyCtx(), current.Order.ID)
		require.NoError(t, err)
		require.Equal(t, current.Operation.ID, view.Operation.ID)
		require.True(t, view.CanRetrySameOperation)
		require.Len(t, r.State().Operations, 2)
		for _, call := range g.Calls() {
			require.Equal(t, "GET", call.Method)
		}
		require.Equal(t, old.Operation.ID, r.State().Bindings[in.RequestKey].OperationID)
	})
	t.Run("last_observed_resume_flag_is_rechecked_on_post", func(t *testing.T) { // LIFE-008 LIFE-002
		c := newPolicyClock()
		v := policyFixture(c, "open")
		e := policyEvidence(v.Operation.Snapshot)
		e.Status = "complete"
		e.PaymentStatus = "unpaid"
		g := newPolicyGateway(policyReply{evidence: e})
		r, s := policyContinue(t, v, g, c)
		view, err := s.Get(policyCtx(), v.Order.ID)
		require.NoError(t, err)
		require.True(t, view.CanResume)
		require.Empty(t, g.Calls())
		_, err = s.Continue(policyCtx(), v.Order.ID, policyKey())
		policyErrorCode(t, err, "checkout_blocked")
		require.Equal(t, "complete_unpaid", r.State().Operations[v.Operation.ID].State)
		require.NotContains(t, r.State().Bindings, policyKey())
		require.Len(t, g.Calls(), 1)
		require.Equal(t, "GET", g.Calls()[0].Method)
	})

}
func TestLIFE009CheckoutRules(t *testing.T) { // LIFE-009
	cases := []struct {
		name     string
		attempts int
		elapsed  time.Duration
	}{
		{"retrieval_exhausts_attempt_budget", 1, 0},               // LIFE-009 STR-005
		{"retrieval_exhausts_elapsed_budget", 3, 7 * time.Second}, // LIFE-009 STR-005
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPolicyClock()
			v := policyFixture(c, "expired")
			r := newPolicyRepository(v)
			g := newPolicyGateway(policyReply{run: func(_ context.Context, _ Snapshot, _ string) (SessionEvidence, error) {
				c.Advance(tc.elapsed)
				e := policyEvidence(v.Operation.Snapshot)
				e.Status = "expired"
				return e, nil
			}}, policyOpenReply())
			opts := policyOptions(c)
			opts.MaxAttempts = tc.attempts
			s := policyService(t, r, g, opts)
			out, err := s.Continue(policyCtx(), v.Order.ID, policyKey())
			require.NoError(t, err)
			require.True(t, out.Pending)
			require.Equal(t, "prepared", out.Operation.State)
			require.Nil(t, out.Operation.FirstDispatchAt)
			require.True(t, out.CanRetrySameOperation)
			require.NotEqual(t, v.Operation.ID, out.Operation.ID)
			require.Len(t, g.Calls(), 1)
			require.Equal(t, "GET", g.Calls()[0].Method)
			require.Equal(t, out.Operation.ID, r.State().Bindings[policyKey()].OperationID)
			require.NotEmpty(t, r.State().History[v.Order.ID])
			foundPrepared := false
			for _, h := range r.State().History[v.Order.ID] {
				if h.OperationID == out.Operation.ID && h.Kind == "operation_prepared" {
					foundPrepared = true
				}
			}
			require.True(t, foundPrepared, "deferred attempt must retain operation_prepared history")
			snapshot := out.Operation.Snapshot
			fresh := policyService(t, r, g, opts)
			retry, err := fresh.Continue(policyCtx(), v.Order.ID, policyKey())
			require.NoError(t, err)
			require.Equal(t, out.Operation.ID, retry.Operation.ID)
			require.Equal(t, out.Operation.StripeKey, retry.Operation.StripeKey)
			require.Equal(t, snapshot.Description, retry.Operation.Snapshot.Description)
			require.Equal(t, snapshot.Amount, retry.Operation.Snapshot.Amount)
			require.Equal(t, snapshot.SuccessURL, retry.Operation.Snapshot.SuccessURL)
			require.Len(t, r.State().Operations, 2)
			require.Len(t, g.Calls(), 2)
			require.Equal(t, "POST", g.Calls()[1].Method)
		})
	}
}
