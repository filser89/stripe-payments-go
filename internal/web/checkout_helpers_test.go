package web

import (
	"context"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/config"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type checkoutFake struct {
	Created   atomic.Int32
	Continued atomic.Int32
	Read      atomic.Int32
	Histories atomic.Int32
	Outcome   payment.Outcome
	View      payment.View
	Page      payment.HistoryPage
	Err       error
	Input     payment.CreateInput
	After     int64
	Limit     int
	Before    func(context.Context)
}

func (f *checkoutFake) Create(ctx context.Context, in payment.CreateInput) (payment.Outcome, error) {
	f.Created.Add(1)
	f.Input = in
	if f.Before != nil {
		f.Before(ctx)
	}
	return f.Outcome, f.Err
}
func (f *checkoutFake) Continue(ctx context.Context, _, _ string) (payment.Outcome, error) {
	f.Continued.Add(1)
	if f.Before != nil {
		f.Before(ctx)
	}
	return f.Outcome, f.Err
}
func (f *checkoutFake) Get(ctx context.Context, _ string) (payment.View, error) {
	f.Read.Add(1)
	if f.Before != nil {
		f.Before(ctx)
	}
	return f.View, f.Err
}
func (f *checkoutFake) History(ctx context.Context, _ string, after int64, limit int) (payment.HistoryPage, error) {
	f.Histories.Add(1)
	f.After = after
	f.Limit = limit
	if f.Before != nil {
		f.Before(ctx)
	}
	return f.Page, f.Err
}
func (f *checkoutFake) Effects() int32 {
	return f.Created.Load() + f.Continued.Load() + f.Read.Load() + f.Histories.Load()
}
func preparedCheckout() *checkoutFake {
	i := testutil.Intent()
	e := testutil.Evidence(i.Operation)
	i.Operation.State = "open"
	i.Operation.SessionID = testutil.Pointer(e.SessionID)
	i.Operation.CheckoutURL = testutil.Pointer(e.URL)
	i.Operation.ExpiresAt = testutil.Pointer(time.Unix(e.ExpiresAt, 0).UTC())
	return &checkoutFake{Outcome: payment.Outcome{Order: i.Order, Operation: i.Operation, NewlyAccepted: true, Established: true, CanResume: true}, View: payment.View{Order: i.Order, Operation: i.Operation, CanResume: true}, Page: payment.HistoryPage{OrderID: i.Order.ID, Entries: []payment.HistoryEntry{}, NextAfter: testutil.Pointer(int64(0))}}
}
func checkoutHandler(t *testing.T, f *checkoutFake, timeout time.Duration) (http.Handler, *testutil.Logs) {
	t.Helper()
	logs := &testutil.Logs{}
	extra := NewCheckoutHandler(f, timeout, logs.Logger())
	require.NotNil(t, extra, "missing checkout HTTP boundary")
	env := testutil.Environment("postgres://fixture:fixture@localhost/fixture", "localhost:8080")
	c, err := config.Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	c.ReadTimeout = timeout + time.Second
	c.WriteTimeout = timeout + time.Second
	s := New(c, logs.Logger(), func(context.Context) error { return nil }, extra)
	return s.Handler, logs
}

type checkoutReadFault struct {
	Data  []byte
	Error error
	Reads atomic.Int32
}

func (b *checkoutReadFault) Read(p []byte) (int, error) {
	b.Reads.Add(1)
	if len(b.Data) > 0 {
		n := copy(p, b.Data)
		b.Data = b.Data[n:]
		return n, b.Error
	}
	return 0, b.Error
}
func (*checkoutReadFault) Close() error { return nil }
func checkoutError(code string, f *checkoutFake) error {
	return &payment.Error{Code: code, OrderID: f.Outcome.Order.ID, OperationID: f.Outcome.Operation.ID, Cause: errors.New("sensitive-sentinel")}
}
