package stripeapi

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/filser89/stripe-payments-go/internal/payment"
	stripe "github.com/stripe/stripe-go/v87"
)

type callError struct {
	class string
	cause error
}

func (e *callError) Error() string { return "stripe call: " + e.class }
func (e *callError) Unwrap() error { return e.cause }
func safeCode(s string) string {
	if len(s) > 100 {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return s
}
func translateError(ctx context.Context, e payment.SessionEvidence, err error, facts *responseFacts) (payment.SessionEvidence, error) {
	e.ErrorClass = "generic"
	if facts.status == 0 {
		e.ErrorClass = "transport"
	}
	e.RequestID = facts.header.Get("Request-Id")
	var se *stripe.Error
	if errors.As(err, &se) {
		if se.LastResponse != nil {
			facts.header = se.LastResponse.Header
			facts.status = se.LastResponse.StatusCode
		}
		if e.RequestID == "" {
			e.RequestID = se.RequestID
		}
		e.ErrorCode = safeCode(string(se.Code))
		switch string(se.Type) {
		case "invalid_request_error":
			e.ErrorClass = "validation"
		case "authentication_error":
			e.ErrorClass = "credential"
		case "permission_error":
			e.ErrorClass = "permission"
		case "rate_limit_error":
			e.ErrorClass = "rate_limit"
		case "idempotency_error":
			e.ErrorClass = "idempotency"
		case "api_error":
			e.ErrorClass = "server"
		}
	}
	if facts.status >= 300 && facts.status < 400 {
		e.ErrorClass = "redirect"
	} else if facts.status >= 500 {
		e.ErrorClass = "server"
	} else if facts.status == 429 {
		e.ErrorClass = "rate_limit"
	} else if facts.status == 409 {
		e.ErrorClass = "generic"
		if e.ErrorCode == "idempotency_key_in_use" {
			e.ErrorClass = "transient_conflict"
		}
	} else if facts.status >= 200 && facts.status < 300 {
		e.ErrorClass = "malformed"
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e.ErrorClass = "transport"
		} else {
			e.ErrorClass = "canceled"
		}
		err = ctx.Err()
	}
	if facts.header != nil {
		retry, eparse := strconv.ParseInt(facts.header.Get("Retry-After"), 10, 64)
		if eparse == nil && retry > 0 && retry <= int64((1<<63-1)/int64(time.Second)) {
			e.RetryAfter = time.Duration(retry) * time.Second
		}
		switch facts.header.Get("Stripe-Should-Retry") {
		case "true":
			decision := true
			e.StripeShouldRetry = &decision
		case "false":
			decision := false
			e.StripeShouldRetry = &decision
		}
	}
	return e, &callError{class: e.ErrorClass, cause: err}
}
