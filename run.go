package circuit

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Run executes fn with circuit breaker protection, returning a typed result.
// The function is called synchronously — the caller controls concurrency.
// If a timeout is configured, it is applied via context.WithTimeout;
// the runner must respect context cancellation for timeouts to take effect.
// Panics in fn are caught, recorded as failures, and re-panicked.
func Run[T any](b *Breaker, ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if b.tracker == nil {
		return zero, ErrNotInitialized
	}

	if err := b.checkFitness(ctx); err != nil {
		return zero, err
	}

	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	start := time.Now()

	defer func() {
		if r := recover(); r != nil {
			b.recordOutcome(fmt.Errorf("panic: %v", r), time.Since(start))
			panic(r)
		}
	}()

	result, err := fn(ctx)
	b.recordOutcome(err, time.Since(start))

	// convert context deadline errors to ErrTimeout for the caller
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return result, ErrTimeout.withContext(b.name, State(atomic.LoadUint32(&b.state)))
	}
	return result, err
}
