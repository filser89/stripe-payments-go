package stripeapi

import (
	"github.com/filser89/stripe-payments-go/internal/payment"
	"net/http"
)

type Options struct {
	BackendURL string
	HTTPClient *http.Client
}

func New(string, Options) payment.Gateway { return nil }
