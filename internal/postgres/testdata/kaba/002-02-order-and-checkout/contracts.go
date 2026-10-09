package postgres

import (
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPaymentRepository(*pgxpool.Pool) payment.Repository { return nil }
