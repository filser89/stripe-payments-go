package testutil

import (
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/google/uuid"
	"time"
)

func Purchase() payment.Order {
	now := time.Date(2026, 10, 9, 12, 0, 0, 123000000, time.UTC)
	return payment.Order{ID: uuid.NewString(), Description: "Single café product", Amount: 2500, Currency: "usd", Status: "unpaid", CreatedAt: now, UpdatedAt: now}
}
func Operation(order payment.Order) payment.Operation {
	now := order.CreatedAt
	id := uuid.NewString()
	snap := payment.Snapshot{OrderID: order.ID, OperationID: id, Description: order.Description, Amount: order.Amount, Currency: order.Currency, StripeKey: uuid.NewString(), SDKVersion: "v87.0.0", APIVersion: "2026-09-30.endive", SuccessURL: "http://localhost:8080/?order_id=" + order.ID + "&checkout_return=success", CancelURL: "http://localhost:8080/?order_id=" + order.ID + "&checkout_return=cancel"}
	return payment.Operation{ID: id, OrderID: order.ID, State: "prepared", StripeKey: snap.StripeKey, Snapshot: snap, CreatedAt: now, UpdatedAt: now, Version: 1}
}
func Binding(o payment.Order, op payment.Operation) payment.RequestBinding {
	return payment.RequestBinding{Key: uuid.NewString(), Method: "POST", Target: "/api/orders", OrderID: o.ID, OperationID: op.ID, Description: o.Description, Amount: o.Amount, Currency: o.Currency, CreatedAt: o.CreatedAt}
}
func Intent() payment.AcceptedIntent {
	o := Purchase()
	op := Operation(o)
	o.CurrentOperationID = op.ID
	return payment.AcceptedIntent{Order: o, Operation: op, Binding: Binding(o, op), History: payment.HistoryEntry{OrderID: o.ID, OperationID: op.ID, Kind: "order_created", ObservedAt: o.CreatedAt}}
}
func Options() payment.Options {
	return payment.Options{Currency: "usd", MinAmount: 50, MaxAmount: 100000, Origin: "http://localhost:8080", SDKVersion: "v87.0.0", APIVersion: "2026-09-30.endive", RequestTimeout: 10 * time.Second, CallTimeout: 2 * time.Second, RetryBudget: 7 * time.Second, MaxAttempts: 3, Now: time.Now}
}
func Pointer[T any](v T) *T { return &v }
func Evidence(op payment.Operation) payment.SessionEvidence {
	return payment.SessionEvidence{SessionID: "cs_test_" + op.ID, ClientReferenceID: op.OrderID, Metadata: map[string]string{"order_id": op.OrderID, "operation_id": op.ID}, AmountTotal: op.Snapshot.Amount, Currency: op.Snapshot.Currency, Mode: "payment", Status: "open", PaymentStatus: "unpaid", URL: "https://checkout.stripe.com/c/pay/cs_test_fixture", ExpiresAt: op.CreatedAt.Add(24 * time.Hour).Unix(), RequestID: "req_fixture", ObservedAt: op.CreatedAt}
}
