// Package stripeapi translates the official SDK without deciding payment policy.
package stripeapi

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"

	"github.com/filser89/stripe-payments-go/internal/payment"
	stripe "github.com/stripe/stripe-go/v87"
)

// Options supplies construction-only backend injection for local integration.
type Options struct {
	BackendURL string
	HTTPClient *http.Client
}
type gateway struct{ client *stripe.Client }
type responseContextKey struct{}
type responseFacts struct {
	header http.Header
	status int
}
type capturingTransport struct{ next http.RoundTripper }

func (t capturingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Preserve the single-use request contract through injected transport wrappers.
	request := r.Clone(r.Context())
	request.Close = true
	request.GetBody = nil
	response, err := t.next.RoundTrip(request)
	if response != nil {
		if facts, ok := r.Context().Value(responseContextKey{}).(*responseFacts); ok {
			facts.header = response.Header.Clone()
			facts.status = response.StatusCode
		}
	}
	return response, err
}

// New gives each client its own backend with retries and SDK diagnostics disabled.
func New(key string, options Options) payment.Gateway {
	client := &http.Client{}
	if options.HTTPClient != nil {
		*client = *options.HTTPClient
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = capturingTransport{next: singleAttemptTransport(transport)}
	config := &stripe.BackendConfig{HTTPClient: client, MaxNetworkRetries: stripe.Int64(0), LeveledLogger: &stripe.LeveledLogger{Level: stripe.LevelNull}}
	if options.BackendURL != "" {
		config.URL = stripe.String(options.BackendURL)
	}
	return &gateway{client: stripe.NewClient(key, stripe.WithBackends(stripe.NewBackendsWithConfig(config)))}
}

// Standard transports use verified TLS and HTTP/1 on fresh connections. This
// excludes HTTP/2 stream retries and HTTP/1 retries after connection reuse.
// Opaque construction-only test transports retain their injected behavior.
func singleAttemptTransport(source http.RoundTripper) http.RoundTripper {
	standard, ok := source.(*http.Transport)
	if !ok {
		return source
	}
	controlled := standard.Clone()
	controlled.DisableKeepAlives = true
	controlled.ForceAttemptHTTP2 = false
	controlled.Protocols = &http.Protocols{}
	controlled.Protocols.SetHTTP1(true)
	controlled.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	if controlled.TLSClientConfig == nil {
		controlled.TLSClientConfig = &tls.Config{}
	} else {
		controlled.TLSClientConfig = controlled.TLSClientConfig.Clone()
	}
	controlled.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return controlled
}

func (g *gateway) Create(ctx context.Context, s payment.Snapshot) (payment.SessionEvidence, error) {
	metadata := map[string]string{"order_id": s.OrderID, "operation_id": s.OperationID}
	params := &stripe.CheckoutSessionCreateParams{
		ClientReferenceID: stripe.String(s.OrderID), Mode: stripe.String("payment"), UIMode: stripe.String("hosted_page"),
		SuccessURL: stripe.String(s.SuccessURL), CancelURL: stripe.String(s.CancelURL), ExpiresAt: stripe.Int64(s.ExpiresAt), Metadata: metadata,
		LineItems:         []*stripe.CheckoutSessionCreateLineItemParams{{Quantity: stripe.Int64(1), PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{Currency: stripe.String(s.Currency), UnitAmount: stripe.Int64(s.Amount), ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{Name: stripe.String(s.Description)}}}},
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{CaptureMethod: stripe.String("automatic"), Metadata: metadata},
		AutomaticTax:      &stripe.CheckoutSessionCreateAutomaticTaxParams{Enabled: stripe.Bool(false)}, AllowPromotionCodes: stripe.Bool(false),
		AdaptivePricing: &stripe.CheckoutSessionCreateAdaptivePricingParams{Enabled: stripe.Bool(false)}, AfterExpiration: &stripe.CheckoutSessionCreateAfterExpirationParams{Recovery: &stripe.CheckoutSessionCreateAfterExpirationRecoveryParams{Enabled: stripe.Bool(false)}},
	}
	params.AddExtra("payment_method_types[0]", "card")
	params.SetIdempotencyKey(s.StripeKey)
	facts := &responseFacts{}
	ctx = context.WithValue(ctx, responseContextKey{}, facts)
	session, err := g.client.V1CheckoutSessions.Create(ctx, params)
	return translate(ctx, session, err, facts)
}
func (g *gateway) Retrieve(ctx context.Context, sessionID string) (payment.SessionEvidence, error) {
	facts := &responseFacts{}
	ctx = context.WithValue(ctx, responseContextKey{}, facts)
	session, err := g.client.V1CheckoutSessions.Retrieve(ctx, sessionID, &stripe.CheckoutSessionRetrieveParams{})
	return translate(ctx, session, err, facts)
}
func translate(ctx context.Context, s *stripe.CheckoutSession, err error, facts *responseFacts) (payment.SessionEvidence, error) {
	e := payment.SessionEvidence{ObservedAt: time.Now().UTC()}
	if facts.status >= 300 && facts.status < 400 && err == nil {
		err = errors.New("stripe redirect response")
	}
	if err != nil {
		return translateError(ctx, e, err, facts)
	}
	e.SessionID = s.ID
	e.ClientReferenceID = s.ClientReferenceID
	e.Metadata = s.Metadata
	e.AmountTotal = s.AmountTotal
	e.Currency = string(s.Currency)
	e.Mode = string(s.Mode)
	e.Livemode = s.Livemode
	e.Status = string(s.Status)
	e.PaymentStatus = string(s.PaymentStatus)
	e.URL = s.URL
	e.ExpiresAt = s.ExpiresAt
	if s.PaymentIntent != nil && s.PaymentIntent.ID != "" {
		e.PaymentIntentID = &s.PaymentIntent.ID
	}
	if s.LastResponse != nil {
		e.RequestID = s.LastResponse.RequestID
	} else {
		e.RequestID = facts.header.Get("Request-Id")
	}
	return e, nil
}
