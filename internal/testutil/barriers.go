package testutil

import (
	"context"
	"sync"
)

type Barrier struct {
	Arrived  chan struct{}
	Released chan struct{}
	Done     chan struct{}
	arrival  sync.Once
	release  sync.Once
	done     sync.Once
}

func NewBarrier() *Barrier {
	return &Barrier{Arrived: make(chan struct{}), Released: make(chan struct{}), Done: make(chan struct{})}
}
func (b *Barrier) Wait(ctx context.Context) error {
	b.arrival.Do(func() { close(b.Arrived) })
	defer b.done.Do(func() { close(b.Done) })
	select {
	case <-b.Released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Barrier) Release() { b.release.Do(func() { close(b.Released) }) }
