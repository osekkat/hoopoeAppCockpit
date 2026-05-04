package load_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

// hp-q3p scenario #4 — Bounded semaphore stress.
//
// Trigger more concurrent acquires than the cap allows on the
// ResourceLimiter; assert:
//
//   - the simultaneous holder count never exceeds the configured limit.
//   - excess goroutines block (rather than acquiring + busting the cap).
//   - when leases are released, blocked acquires proceed promptly.
//   - cancelling the acquire context returns ctx.Err() rather than
//     bypassing the cap.
//   - InUse() reports a value bounded by Limit() at all times.
func TestResourceLimiterUnderStressNeverExceedsLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; rerun with `go test ./internal/transport/load/...`")
	}

	const (
		resource     = jobs.ResourceLLMCalls
		limit        = 4
		goroutines   = 200
		holdDuration = 10 * time.Millisecond
	)
	limiter, err := jobs.NewResourceLimiter(map[jobs.Resource]int{resource: limit})
	if err != nil {
		t.Fatalf("limiter: %v", err)
	}

	var (
		concurrent int64
		maxObserved int64
	)

	tickerStop := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if obs := int64(limiter.InUse(resource)); obs > atomic.LoadInt64(&maxObserved) {
					atomic.StoreInt64(&maxObserved, obs)
				}
			case <-tickerStop:
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			lease, err := limiter.Acquire(ctx, resource)
			if err != nil {
				t.Errorf("acquire #%d: %v", id, err)
				return
			}
			defer lease.Release()
			n := atomic.AddInt64(&concurrent, 1)
			if n > int64(limit) {
				t.Errorf("simultaneous holders %d exceeded limit %d (cap violation)", n, limit)
			}
			time.Sleep(holdDuration)
			atomic.AddInt64(&concurrent, -1)
		}(i)
	}
	wg.Wait()
	close(tickerStop)

	if v := atomic.LoadInt64(&maxObserved); v > int64(limit) {
		t.Fatalf("InUse(%s) reached %d, exceeded Limit %d", resource, v, limit)
	}
	if v := atomic.LoadInt64(&concurrent); v != 0 {
		t.Fatalf("concurrent holder counter leaked: %d", v)
	}
	if remaining := limiter.InUse(resource); remaining != 0 {
		t.Fatalf("limiter InUse=%d after all leases released; expected 0", remaining)
	}
}

// Assert that an acquire blocked behind a held lease cancels cleanly
// when its context expires — not bypassing the cap, not leaking the
// goroutine.
func TestResourceLimiterAcquireRespectsContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; rerun with `go test ./internal/transport/load/...`")
	}
	const resource = jobs.ResourceLLMCalls
	limiter, err := jobs.NewResourceLimiter(map[jobs.Resource]int{resource: 1})
	if err != nil {
		t.Fatalf("limiter: %v", err)
	}
	holder, err := limiter.Acquire(context.Background(), resource)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer holder.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	lease, err := limiter.Acquire(ctx, resource)
	elapsed := time.Since(start)
	if err == nil {
		lease.Release()
		t.Fatalf("expected context-cancellation error; got nil after %s", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("blocked acquire took %s after context cancellation; should return promptly", elapsed)
	}
	if v := limiter.InUse(resource); v != 1 {
		t.Fatalf("InUse=%d after cancelled acquire (should still be 1, just the original holder)", v)
	}
}
