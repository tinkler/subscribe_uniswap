package event

import (
	"sync/atomic"

	"github.com/ethereum/go-ethereum/ethclient"
)

type updateEvent struct {
	running int32
	d       chan struct{}
	client  *ethclient.Client
}

func NewUpdateEvent(client *ethclient.Client) *updateEvent {
	return &updateEvent{
		d:      make(chan struct{}),
		client: client,
	}
}

func (e *updateEvent) Trigger() {
	e.d <- struct{}{}
}

func (e *updateEvent) Run(f func(client *ethclient.Client)) {
	if atomic.CompareAndSwapInt32(&e.running, 0, 1) {
		go func() {
			for range e.d {
				f(e.client)
			}
		}()
	}
}

func (e *updateEvent) Close() error {
	close(e.d)
	e.client.Close()
	return nil
}
