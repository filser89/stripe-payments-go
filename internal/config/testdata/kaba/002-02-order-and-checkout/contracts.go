package config

import "time"

type CheckoutConfig struct {
	SecretKey      string
	BaseURL        string
	Currency       string
	MinAmount      int64
	MaxAmount      int64
	RequestTimeout time.Duration
	CallTimeout    time.Duration
	RetryBudget    time.Duration
	MaxAttempts    int
}

func (Config) CheckoutSettings() (out CheckoutConfig) { return }
