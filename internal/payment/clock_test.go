package payment

import (
	"context"
	"sync"
	"time"
)

type policyClock struct {
	mu       sync.Mutex
	at       time.Time
	waits    []time.Duration
	waitHook func(context.Context, time.Duration) error
}

func newPolicyClock() *policyClock {
	return &policyClock{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
}
func (c *policyClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *policyClock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.at = c.at.Add(d) }
func (c *policyClock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.waits = append(c.waits, d)
	hook := c.waitHook
	c.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, d); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.Advance(d)
	return nil
}
func (c *policyClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}
