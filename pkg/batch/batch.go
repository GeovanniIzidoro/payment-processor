// Package batch implements a dynamic batcher. Callers submit items.
// Individually, and the batcher groups them by size or time window.
// Whichever happens first. This trades a small bounded latency for a large gain in throughput on the downstream call.
package batch

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrClosed = errors.New("batch: batcher is closed")

// Flusher processes a full batch. The implementation must be safe for
// concurrent calls because two windows can overlap under load.
type Flusher[In any, Out any] func(ctx context.Context, items []In) ([]Out, error)

type Batcher[In any, Out any] struct {
	maxSize int
	maxWait time.Duration
	flusher Flusher[In, Out]

	mu     sync.Mutex
	queue  chan request[In, Out]
	closed bool
	wg     sync.WaitGroup
}

type request[In any, Out any] struct {
	items In
	resp  chan result[Out]
}

type result[Out any] struct {
	err   error
	value Out
}

// New creates a new Batcher with the given maximum batch size, maximum wait time, and flusher function.
func New[In any, Out any](maxSize int, maxWait time.Duration, flusher Flusher[In, Out]) *Batcher[In, Out] {
	if maxSize < 1 {
		maxSize = 1
	}

	b := &Batcher[In, Out]{
		maxSize: maxSize,
		maxWait: maxWait,
		flusher: flusher,
		queue:   make(chan request[In, Out], maxSize*4),
	}
	b.wg.Add(1)
	go b.loop()

	return b
}

// Submit submits an item to the batcher and waits for the result. It returns an error if the batcher is closed or if the context is canceled.
func (b *Batcher[In, Out]) Submit(ctx context.Context, item In) (Out, error) {
	var zero Out

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return zero, ErrClosed
	}

	req := request[In, Out]{items: item, resp: make(chan result[Out], 1)}
	b.queue <- req
	b.mu.Unlock()

	select {
	case res := <-req.resp:
		return res.value, res.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Close closes the batcher and waits for all pending requests to be processed. After calling Close, no more requests can be submitted.
func (b *Batcher[In, Out]) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}

	b.closed = true
	close(b.queue)
	b.mu.Unlock()

	b.wg.Wait()
}

// dispatch sends a batch of requests to the flusher and sends the results back to the requesters.
func (b *Batcher[In, Out]) dispatch(batch []request[In, Out]) {
	items := make([]In, len(batch))
	for i, req := range batch {
		items[i] = req.items
	}

	outs, err := b.flusher(context.Background(), items)
	if err == nil && len(outs) != len(batch) {
		err = errors.New("batch: flusher returned wrong number of results")
	}

	for i, req := range batch {
		if err != nil {
			var zero Out
			req.resp <- result[Out]{err: err, value: zero}
			continue
		}
		req.resp <- result[Out]{err: nil, value: outs[i]}
	}
}

// loop is the main loop of the batcher. It collects requests and dispatches them in batches based on size or time window.
func (b *Batcher[In, Out]) loop() {
	defer b.wg.Done()

	pending := make([]request[In, Out], 0, b.maxSize)
	timer := time.NewTimer(b.maxWait)
	defer timer.Stop()

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending = make([]request[In, Out], 0, b.maxSize)
		b.dispatch(batch)
	}

	for {
		select {
		case req, ok := <-b.queue:
			if !ok {
				flushPending()
				return
			}

			pending = append(pending, req)
			if len(pending) == 1 {
				timer.Reset(b.maxWait)
			}

			if len(pending) >= b.maxSize {
				flushPending()
			}
		case <-timer.C:
			flushPending()
		}
	}
}
