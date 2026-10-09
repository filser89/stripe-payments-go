package testutil

import (
	"context"
	"sync"
	"time"
)

type Clock struct {
	mu    sync.Mutex
	Time  time.Time
	Waits []time.Duration
}

func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.Time }
func (c *Clock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Waits = append(c.Waits, d)
	c.Time = c.Time.Add(d)
	return nil
}
