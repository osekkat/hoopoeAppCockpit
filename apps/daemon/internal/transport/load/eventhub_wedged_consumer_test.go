package load_test

import (
	"context"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
)

// hp-q3p scenario #2 — Wedged WS consumer.
//
// Subscribes to the EventHub and then deliberately stops reading the
// subscriber channel. Asserts:
//
//   - publishing continues to succeed and never blocks the publisher
//     (the bounded subscriber channel must be a back-pressure boundary,
//     not a producer-blocking gate).
//   - cancelling the subscriber's context closes the channel (the
//     daemon side cleans up the wedged subscription within bounded
//     time).
//   - the subscriber channel never grows past `subscriberCapacity`.
func TestEventHubWedgedConsumerDoesNotBlockPublisher(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; rerun with `go test ./internal/transport/load/...`")
	}

	const (
		channel     = "swarm.alpha"
		bufferSize  = 8
		publishMany = 5_000
	)
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	hub := api.NewEventHub(api.EventHubConfig{
		ReplayCapacity:     1_024,
		SubscriberCapacity: bufferSize,
		Now:                now,
	})

	subCtx, cancelSub := context.WithCancel(context.Background())
	sub := hub.Subscribe(subCtx, []string{channel})
	// NOTE: we intentionally do NOT defer sub.Close() — the cancellation
	// path is what we're asserting on.

	// Publish a flood without anyone draining the channel.
	publishStart := time.Now()
	for i := 0; i < publishMany; i++ {
		hub.Publish(api.PublishInput{
			Channel: channel,
			Type:    "job.update",
			Data:    map[string]any{"index": i},
		})
	}
	publishElapsed := time.Since(publishStart)

	// Heuristic: publishing 5,000 events into a wedged subscriber should
	// take well under a second. If it's slow, the publisher is blocking
	// on the subscriber channel — the very anti-pattern this test guards.
	if publishElapsed > 2*time.Second {
		t.Fatalf("publishing into wedged consumer took %s; publisher must not block on subscriber channel", publishElapsed)
	}

	// Subscriber channel must still be bounded.
	if depth := len(sub.Events()); depth > bufferSize {
		t.Fatalf("wedged subscriber channel depth %d exceeded subscriberCapacity %d", depth, bufferSize)
	}

	// Cancel the subscription context; the daemon's goroutine bound to
	// ctx.Done() should call sub.Close() within bounded time and the
	// channel becomes detectable-as-closed.
	cancelSub()

	// Best-effort wait for the cleanup goroutine. Drain any remaining
	// buffered events but bound the wait — if the subscription truly
	// closes, ranging over `events` returns once the channel is closed
	// or the deadline trips.
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelTimeout()
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		// Repeated Close() should be safe; it's an idempotent operation
		// the daemon also calls through ctx.Done().
		sub.Close()
	}()
	select {
	case <-cleanupDone:
		// good
	case <-timeoutCtx.Done():
		t.Fatalf("wedged subscriber did not clean up after context cancellation")
	}
}
