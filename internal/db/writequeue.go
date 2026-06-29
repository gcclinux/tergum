package db

import (
	"context"
	"sync"
)

// WriteQueue serializes database write operations through a single goroutine
// to prevent SQLITE_BUSY errors when multiple gRPC streams attempt concurrent
// writes. Reads are unaffected and can proceed concurrently (WAL mode).
type WriteQueue struct {
	ch     chan writeRequest
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// writeRequest wraps a write operation and a channel to receive the result.
type writeRequest struct {
	fn     func() error
	result chan<- error
}

// NewWriteQueue creates and starts a write queue with the given buffer size.
// A buffer of 256 allows callers to enqueue without blocking under normal load.
func NewWriteQueue(bufferSize int) *WriteQueue {
	if bufferSize <= 0 {
		bufferSize = 256
	}

	ctx, cancel := context.WithCancel(context.Background())
	q := &WriteQueue{
		ch:     make(chan writeRequest, bufferSize),
		cancel: cancel,
	}

	q.wg.Add(1)
	go q.loop(ctx)

	return q
}

// loop processes write requests sequentially. It exits when the context is
// cancelled and the channel is drained.
func (q *WriteQueue) loop(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case req, ok := <-q.ch:
			if !ok {
				return
			}
			req.result <- req.fn()
		case <-ctx.Done():
			// Drain remaining requests so callers don't hang.
			for {
				select {
				case req, ok := <-q.ch:
					if !ok {
						return
					}
					req.result <- context.Canceled
				default:
					return
				}
			}
		}
	}
}

// Submit enqueues a write operation and blocks until it completes.
// Returns the error from the write function, or context.Canceled if the
// caller's context expires before the operation is processed.
func (q *WriteQueue) Submit(ctx context.Context, fn func() error) error {
	result := make(chan error, 1)
	req := writeRequest{fn: fn, result: result}

	select {
	case q.ch <- req:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the queue and waits for pending operations to drain.
func (q *WriteQueue) Close() {
	q.cancel()
	close(q.ch)
	q.wg.Wait()
}
