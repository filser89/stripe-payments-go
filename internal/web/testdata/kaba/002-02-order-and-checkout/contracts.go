package web

import (
	"github.com/filser89/stripe-payments-go/internal/payment"
	"log/slog"
	"net/http"
	"time"
)

func NewCheckoutHandler(payment.Operations, time.Duration, *slog.Logger) http.Handler { return nil }
