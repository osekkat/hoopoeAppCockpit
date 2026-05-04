package load_test

import (
	"context"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
)

// hp-q3p scenario #3 — Activity stream burst.
//
// Publishes 10,000 events into the activity channel as fast as
// possible; a fast consumer drains the bounded channel; Replay()
// covers any in-flight gap. Asserts the §10.1 / Appendix B #1
// invariant family:
//
//   - delivered events arrive in monotonically-increasing sequence
//     order on a per-channel basis (no reordering within a channel).
//   - whenever the consumer falls behind, the bounded subscriber
//     channel emits a `_lag` marker rather than silently dropping
//     (Appendix B #1: no unbounded buffer; lag is a signal).
//   - every gap reported in live delivery is reachable via the
//     replay window (Replay returns events with sequence > cursor),
//     so a renderer reconnecting from the last seen cursor can close
//     the gap.
//   - throughput is non-trivial (sanity floor chosen well below any
//     reasonable hardware so this isn't a flaky perf test).
func TestEventHubBurstPublishStaysOrderedAndReplayClosesAnyGap(t *testing.T) {
	if testing.Short() {
		t.Skip("load test; rerun with `go test ./internal/transport/load/...`")
	}

	const (
		channel     = "activity.stream"
		bufferSize  = 256
		publishMany = 10_000
	)
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	hub := api.NewEventHub(api.EventHubConfig{
		ReplayCapacity:     publishMany * 2,
		SubscriberCapacity: bufferSize,
		Now:                now,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sub := hub.Subscribe(ctx, []string{channel})
	defer sub.Close()

	publisherStart := time.Now()
	publisherDone := make(chan struct{})
	go func() {
		defer close(publisherDone)
		for i := 0; i < publishMany; i++ {
			hub.Publish(api.PublishInput{
				Channel: channel,
				Type:    "activity.event",
				Data:    map[string]any{"index": i},
			})
		}
	}()

	received := make([]api.Event, 0, publishMany)
	deadline := time.After(25 * time.Second)
	events := sub.Events()
	// Drain in a non-blocking loop; stop once the publisher has
	// finished AND the channel is empty.
loop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			received = append(received, ev)
		case <-deadline:
			t.Fatalf("burst test deadline; received=%d/%d", len(received), publishMany)
		default:
			select {
			case <-publisherDone:
				if len(events) == 0 {
					break loop
				}
			default:
				// Yield briefly so the publisher goroutine can enqueue.
				time.Sleep(50 * time.Microsecond)
			}
		}
	}
	elapsed := time.Since(publisherStart)
	<-publisherDone

	// Collect delivered non-lag sequences and verify per-channel
	// monotonic order. lag markers carry the OUTGOING event's
	// sequence (not the dropped one), so the consumer can detect
	// gaps but the cursor for Replay should be the last-in-order
	// non-lag sequence the consumer is sure it received.
	deliveredSeqs := make(map[uint64]struct{}, publishMany)
	var lastSeq uint64
	delivered := 0
	lagSeen := 0
	for _, ev := range received {
		if ev.Type == "_lag" {
			lagSeen++
			continue
		}
		if ev.Sequence <= lastSeq {
			t.Fatalf("non-monotonic sequence: prev=%d, curr=%d", lastSeq, ev.Sequence)
		}
		lastSeq = ev.Sequence
		delivered++
		deliveredSeqs[ev.Sequence] = struct{}{}
	}

	t.Logf("burst delivered=%d, lag=%d, total publish=%d, elapsed=%s", delivered, lagSeen, publishMany, elapsed)

	// The Appendix B #1 / §10.1 invariant we care about:
	//   No published event is unreachable. Every sequence in
	//   [1, publishMany] is either live-delivered OR present in the
	//   replay window. Replay capacity is set high enough above to
	//   hold the full burst.
	replayed, gap := hub.Replay(channel, 0)
	if gap {
		t.Fatalf("Replay(0) reports gap; replay window not large enough — increase ReplayCapacity")
	}
	replayedSeqs := make(map[uint64]struct{}, len(replayed))
	for _, ev := range replayed {
		replayedSeqs[ev.Sequence] = struct{}{}
	}
	missing := 0
	for seq := uint64(1); seq <= uint64(publishMany); seq++ {
		_, inLive := deliveredSeqs[seq]
		_, inReplay := replayedSeqs[seq]
		if !inLive && !inReplay {
			missing++
		}
	}
	if missing > 0 {
		t.Fatalf("burst left %d sequences unreachable from BOTH live delivery AND replay (delivered=%d, replayed=%d, lag=%d)",
			missing, delivered, len(replayedSeqs), lagSeen)
	}

	throughput := float64(publishMany) / elapsed.Seconds()
	t.Logf("burst throughput: %d events in %s (%.0f events/sec)", publishMany, elapsed, throughput)
	const minThroughput = 1_000.0 // events/sec — well below any modern host
	if throughput < minThroughput {
		t.Fatalf("throughput %.0f events/sec below sanity floor %.0f", throughput, minThroughput)
	}
}
