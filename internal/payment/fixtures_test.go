package payment

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func policyPtr[T any](v T) *T { return &v }
func policyOptions(c *policyClock) Options {
	return Options{Currency: "usd", MinAmount: 50, MaxAmount: 100000, Origin: "http://localhost:8080", SDKVersion: "v87.0.0", APIVersion: "2026-09-30.endive", RequestTimeout: 10 * time.Second, CallTimeout: 2 * time.Second, RetryBudget: 7 * time.Second, MaxAttempts: 3, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Now: c.Now, Wait: c.Wait}
}
func policyInput() CreateInput {
	return CreateInput{Description: "One product", Amount: 4200, RequestKey: "42d6c2f8-a529-40ef-8a6a-61ae796227a3"}
}
func policyKey() string { return "6bce8391-3180-4cd9-843d-c00df4b30c13" }
func policyFixture(c *policyClock, state string) View {
	at := c.Now()
	first := at.Add(-time.Minute)
	expiry := first.Add(23*time.Hour + 59*time.Minute)
	o := Order{ID: "e8d54778-7688-4b68-aadf-55cd860911bd", Description: "One product", Amount: 4200, Currency: "usd", Status: "unpaid", CurrentOperationID: "9e7d0ed0-8cf9-47b2-b511-7bb6d075a10c", CreatedAt: first, UpdatedAt: first}
	s := Snapshot{OrderID: o.ID, OperationID: o.CurrentOperationID, Description: o.Description, Amount: o.Amount, Currency: o.Currency, StripeKey: "stripe-independent-operation-key", SuccessURL: "http://localhost:8080/?order_id=" + o.ID + "&checkout_return=success", CancelURL: "http://localhost:8080/?order_id=" + o.ID + "&checkout_return=cancel", SDKVersion: "v87.0.0", APIVersion: "2026-09-30.endive", FirstDispatchAt: &first, LastDispatchAt: &first, ExpiresAt: expiry.Unix()}
	op := Operation{ID: o.CurrentOperationID, OrderID: o.ID, State: state, StripeKey: s.StripeKey, Snapshot: s, CreatedAt: first, UpdatedAt: first, Version: 1, FirstDispatchAt: &first, LastDispatchAt: &first}
	if state == "prepared" {
		op.FirstDispatchAt = nil
		op.LastDispatchAt = nil
		op.Snapshot.FirstDispatchAt = nil
		op.Snapshot.LastDispatchAt = nil
		op.Snapshot.ExpiresAt = 0
	}
	if state == "open" || state == "expired" || state == "complete_unpaid" || state == "paid" {
		op.SessionID = policyPtr("cs_test_policy")
		op.ExpiresAt = &expiry
		op.CheckoutURL = policyPtr("https://checkout.stripe.com/c/pay/cs_test_policy")
	}
	if state == "paid" {
		o.Status = "paid"
		op.PaymentIntentID = policyPtr("pi_policy")
	}
	return View{Order: o, Operation: op}
}
func policyEvidence(s Snapshot) SessionEvidence {
	return SessionEvidence{SessionID: "cs_test_policy", ClientReferenceID: s.OrderID, Metadata: map[string]string{"order_id": s.OrderID, "operation_id": s.OperationID}, AmountTotal: s.Amount, Currency: s.Currency, Mode: "payment", Status: "open", PaymentStatus: "unpaid", URL: "https://checkout.stripe.com/c/pay/cs_test_policy", ExpiresAt: s.ExpiresAt, RequestID: "req_policy", ObservedAt: time.Unix(s.ExpiresAt, 0).Add(-23 * time.Hour)}
}
func policyService(t *testing.T, r *policyRepository, g *policyGateway, opts Options) *Service {
	t.Helper()
	s := New(r, g, opts)
	require.NotNil(t, s, "payment.New must construct the application operations boundary")
	return s
}
func policyErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var e *Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, code, e.Code)
}
func policyAssertUnpaid(t *testing.T, out Outcome) {
	t.Helper()
	require.NotEmpty(t, out.Order.ID)
	require.NotEmpty(t, out.Operation.ID)
	require.Equal(t, "unpaid", out.Order.Status)
	require.Equal(t, out.Order.ID, out.Operation.OrderID)
}
func policyAssertNoEffects(t *testing.T, r *policyRepository, g *policyGateway, before policyRepoState) {
	t.Helper()
	require.Equal(t, before, r.State())
	require.Empty(t, g.Calls())
}
func policyContinue(t *testing.T, v View, g *policyGateway, c *policyClock) (*policyRepository, *Service) {
	t.Helper()
	r := newPolicyRepository(v)
	return r, policyService(t, r, g, policyOptions(c))
}
func policyCtx() context.Context { return context.Background() }

// Each evidence mutation is exercised at all three public operation paths because
// an implementation must not validate only the first successful creation response.
func policyInvalidEvidence(t *testing.T, mutate func(*SessionEvidence), source string) {
	t.Helper()
	c := newPolicyClock()
	r := newPolicyRepository()
	v := policyFixture(c, "unresolved")
	if source == "retrieve" {
		v = policyFixture(c, "open")
	}
	if source != "initial" {
		r = newPolicyRepository(v)
		in := policyInput()
		r.state.Bindings[in.RequestKey] = RequestBinding{Key: in.RequestKey, Method: "POST", Target: "/api/orders", OrderID: v.Order.ID, OperationID: v.Operation.ID, Description: in.Description, Amount: in.Amount, Currency: "usd"}
	}
	reply := policyReply{run: func(_ context.Context, s Snapshot, _ string) (SessionEvidence, error) {
		if source == "retrieve" {
			s = v.Operation.Snapshot
		}
		e := policyEvidence(s)
		mutate(&e)
		return e, nil
	}}
	g := newPolicyGateway(reply)
	service := policyService(t, r, g, policyOptions(c))
	var out Outcome
	var err error
	if source == "retrieve" {
		out, err = service.Continue(policyCtx(), v.Order.ID, policyKey())
		policyErrorCode(t, err, "checkout_blocked")
		require.NotContains(t, r.State().Bindings, policyKey())
	} else {
		out, err = service.Create(policyCtx(), policyInput())
		require.NoError(t, err)
		policyAssertUnpaid(t, out)
		require.True(t, out.Pending)
		require.Equal(t, "unresolved", out.Operation.State)
		require.True(t, out.NeedsInvestigation)
		require.False(t, out.CanResume)
		require.False(t, out.CanStartNewAttempt)
		require.Nil(t, out.Operation.CheckoutURL)
	}
	st := r.State()
	require.Len(t, st.Orders, 1)
	require.Len(t, st.Operations, 1)
	for _, order := range st.Orders {
		require.Equal(t, "unpaid", order.Status)
		view, readErr := service.Get(policyCtx(), order.ID)
		require.NoError(t, readErr)
		require.Equal(t, "unresolved", view.Operation.State)
		require.True(t, view.NeedsInvestigation)
		require.False(t, view.CanResume)
		require.False(t, view.CanStartNewAttempt)
		require.Nil(t, view.Operation.CheckoutURL)
	}
	require.Len(t, g.Calls(), 1)
	method := "POST"
	if source == "retrieve" {
		method = "GET"
	}
	require.Equal(t, method, g.Calls()[0].Method)
}
