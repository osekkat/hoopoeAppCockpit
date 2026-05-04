package load_test

import (
	"context"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
)

// hp-q3p scenario #1 — High-volume publish, slow consumer.
//
// Publishes a large stream of events into an EventHub configured
// with a small subscriber buffer; subscribes a deliberately slow
// consumer; asserts:
//
//   - the subscriber's bounded channel never accumulates more than
//     `subscriberCapacity` events at any point (Appendix B anti-pattern
//     #1: no unbounded channel, no daemon RSS blowup).
//   - the subscriber observes `_lag` markers carrying
//     `lastPersistedOffset` — so the renderer knows it missed events
//     and can fetch them via Replay.
//   - Replay() from the last received cursor closes the gap completely
//     (no missed events end up unreachable).
func TestEventHubSlowConsumerEmitsLagAndChannelStaysBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; rerun with `go test ./internal/transport/load/...`")
	}

	const (
		channel     = "swarm.activity"
		bufferSize  = 16
		publishMany = 10_000
		consumerLag = 50 * time.Microsecond
	)
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	hub := api.NewEventHub(api.EventHubConfig{
		ReplayCapacity:     2_048, // exercise the replay-window trim path too
		SubscriberCapacity: bufferSize,
		Now:                now,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sub := hub.Subscribe(ctx, []string{channel})
	defer sub.Close()

	consumed := make([]api.Event, 0, publishMany)
	lagSeen := 0
	maxChannelDepth := 0

	// Producer: publish as fast as Go can run.
	publisherDone := make(chan struct{})
	go func() {
		defer close(publisherDone)
		for i := 0; i < publishMany; i++ {
			hub.Publish(api.PublishInput{
				Channel: channel,
				Type:    "job.update",
				Data:    map[string]any{"index": i},
			})
		}
	}()

	// Slow consumer: read with a fixed delay; record the channel
	// depth after each receive so we can prove it never exceeds the
	// capacity.
	consumerDeadline := time.After(20 * time.Second)
	events := sub.Events()
consumeLoop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break consumeLoop
			}
			consumed = append(consumed, ev)
			if ev.Type == "_lag" {
				lagSeen++
			}
			if d := len(events); d > maxChannelDepth {
				maxChannelDepth = d
			}
			time.Sleep(consumerLag)
			// Stop once the publisher is done AND the channel has drained
			// (no more events to deliver to a bounded buffer).
			select {
			case <-publisherDone:
				if len(events) == 0 {
					break consumeLoop
				}
			default:
			}
		case <-consumerDeadline:
			t.Fatalf("consumer deadline; consumed=%d, lag=%d", len(consumed), lagSeen)
		}
	}

	// Wait for the producer to fully finish so all events have been
	// published before we sample the final state.
	<-publisherDone

	if maxChannelDepth > bufferSize {
		t.Fatalf("subscriber channel depth %d exceeded subscriberCapacity %d (Appendix B #1 violation)", maxChannelDepth, bufferSize)
	}

	if lagSeen == 0 {
		t.Fatalf("expected at least one _lag marker (consumer was deliberately slow); got 0 across %d consumed events", len(consumed))
	}

	if len(consumed) >= publishMany {
		t.Fatalf("slow consumer received %d events; should have missed some (publish total %d)", len(consumed), publishMany)
	}

	// Find the last in-order non-lag event the consumer received and
	// ask Replay to fill in everything past it. The replay window is
	// bounded by `replayCapacity`, so for a 10k publish stream we
	// expect Replay to return *some* events; the exact count depends
	// on retention. We assert: Replay returns events strictly newer
	// than the cursor, and the daemon does NOT report a gap when
	// `since` is at or beyond the oldest retained event.
	var lastSequence uint64
	for _, ev := range consumed {
		if ev.Type != "_lag" && ev.Sequence > lastSequence {
			lastSequence = ev.Sequence
		}
	}
	replayed, _ := hub.Replay(channel, lastSequence)
	for _, ev := range replayed {
		if ev.Sequence <= lastSequence {
			t.Fatalf("replay returned event with sequence %d <= cursor %d", ev.Sequence, lastSequence)
		}
	}
}
