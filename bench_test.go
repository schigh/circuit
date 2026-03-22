package circuit

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkRun(b *testing.B) {
	breaker, err := NewBreaker(WithName("bench"))
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	fn := func(ctx context.Context) (any, error) {
		return nil, nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Run(breaker, ctx, fn)
		}
	})
}

func BenchmarkAllow(b *testing.B) {
	breaker, err := NewBreaker(WithName("bench"))
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done, err := breaker.Allow(ctx)
			if err != nil {
				b.Fatal(err)
			}
			done(nil)
		}
	})
}

func BenchmarkApplyThrottle(b *testing.B) {
	breaker, err := NewBreaker(WithName("bench"), WithBackOff(time.Second), WithWindow(50*time.Millisecond))
	if err != nil {
		b.Fatal(err)
	}
	// Force into throttled state
	breaker.tracker.incr()
	breaker.State() // closed -> open
	time.Sleep(60 * time.Millisecond)
	breaker.State() // open -> throttled
	atomic.StoreInt64(&breaker.throttledSince, time.Now().UnixNano())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = breaker.applyThrottle()
		}
	})
}

func BenchmarkCheckFitness(b *testing.B) {
	breaker, err := NewBreaker(WithName("bench"))
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = breaker.checkFitness(ctx)
		}
	})
}

func BenchmarkSnapshot(b *testing.B) {
	breaker, err := NewBreaker(WithName("bench"))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = breaker.Snapshot()
		}
	})
}
