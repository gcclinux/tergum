package grpc

import "context"

// Semaphore provides a concurrency limiter using a buffered channel.
// It blocks when the maximum number of concurrent operations is reached.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a new semaphore with the given maximum concurrency.
func NewSemaphore(max int) *Semaphore {
	if max <= 0 {
		max = 1
	}
	return &Semaphore{ch: make(chan struct{}, max)}
}

// Acquire blocks until a slot is available or the context is cancelled.
// Returns nil on success, or the context error if cancelled while waiting.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns true if acquired, false if the semaphore is full.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release releases a slot back to the semaphore.
func (s *Semaphore) Release() {
	<-s.ch
}

// Available returns the number of currently available slots.
func (s *Semaphore) Available() int {
	return cap(s.ch) - len(s.ch)
}
