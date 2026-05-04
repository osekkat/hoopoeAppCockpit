package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const (
	defaultReplayCapacity     = 1024
	defaultSubscriberCapacity = 64
)

// Event is the daemon WebSocket/SSE envelope from plan.md section 2.6.
type Event struct {
	EventID       string         `json:"eventId"`
	SchemaVersion int            `json:"schemaVersion"`
	Channel       string         `json:"channel"`
	Type          string         `json:"type"`
	Sequence      uint64         `json:"sequence"`
	Time          string         `json:"time"`
	Actor         map[string]any `json:"actor,omitempty"`
	CausationID   string         `json:"causationId,omitempty"`
	CorrelationID string         `json:"correlationId,omitempty"`
	Data          any            `json:"data"`
}

type EventHubConfig struct {
	ReplayCapacity     int
	SubscriberCapacity int
	Now                func() time.Time
	// Redactor scrubs Publish.Data before the event is appended to the
	// replay buffer or delivered to subscribers. Mirrors audit/writer.go's
	// pre-write redaction so secret-shaped strings in commit messages,
	// agent-mail bodies, or any future producer cannot reach WS/SSE
	// subscribers raw. nil triggers a default redactor in NewEventHub —
	// EventHub is safe-by-default; opt-out requires the explicit
	// NewEventHubWithoutRedactor escape hatch used by load/chaos fixtures
	// asserting raw delivery semantics.
	Redactor *redaction.Redactor
}

type EventHub struct {
	mu sync.RWMutex

	now                func() time.Time
	replayCapacity     int
	subscriberCapacity int
	sequences          map[string]uint64
	events             []Event
	subscribers        map[uint64]*subscriber
	nextSubscriberID   uint64
	redactor           *redaction.Redactor
}

type PublishInput struct {
	Channel       string
	Type          string
	Actor         map[string]any
	CausationID   string
	CorrelationID string
	Data          any
}

type Subscriber struct {
	hub *EventHub
	sub *subscriber
}

type subscriber struct {
	id       uint64
	channels map[string]struct{}
	events   chan Event
}

type Snapshot struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Time          string                     `json:"time"`
	Channels      map[string]ChannelSnapshot `json:"channels"`
}

type ChannelSnapshot struct {
	LastSequence uint64  `json:"lastSequence"`
	Cursor       uint64  `json:"cursor"`
	Replayed     []Event `json:"replayed"`
	Gap          bool    `json:"gap"`
	Repair       string  `json:"repair,omitempty"`
}

type ReplayResponse struct {
	SchemaVersion int     `json:"schemaVersion"`
	Channel       string  `json:"channel"`
	SinceSequence uint64  `json:"sinceSequence"`
	Gap           bool    `json:"gap"`
	Events        []Event `json:"events"`
}

func NewEventHub(cfg EventHubConfig) *EventHub {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	redactor := cfg.Redactor
	if redactor == nil {
		redactor = redaction.New(redaction.Config{Now: now})
	}
	return newEventHub(cfg, now, redactor)
}

// NewEventHubWithoutRedactor constructs an EventHub that delivers Publish.Data
// verbatim. Reserved for load/chaos test fixtures asserting raw delivery
// semantics where the inputs are known-clean. Production wiring must use
// NewEventHub.
func NewEventHubWithoutRedactor(cfg EventHubConfig) *EventHub {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return newEventHub(cfg, now, nil)
}

func newEventHub(cfg EventHubConfig, now func() time.Time, redactor *redaction.Redactor) *EventHub {
	replayCapacity := cfg.ReplayCapacity
	if replayCapacity <= 0 {
		replayCapacity = defaultReplayCapacity
	}
	subscriberCapacity := cfg.SubscriberCapacity
	if subscriberCapacity <= 0 {
		subscriberCapacity = defaultSubscriberCapacity
	}
	return &EventHub{
		now:                now,
		replayCapacity:     replayCapacity,
		subscriberCapacity: subscriberCapacity,
		sequences:          make(map[string]uint64),
		events:             make([]Event, 0, replayCapacity),
		subscribers:        make(map[uint64]*subscriber),
		redactor:           redactor,
	}
}

func (h *EventHub) Publish(input PublishInput) Event {
	if input.Channel == "" {
		input.Channel = "_system"
	}
	if input.Type == "" {
		input.Type = "daemon.event"
	}

	// Redact event Data before persistence + delivery. Mirrors
	// audit.Writer.redactEntry — the EventHub is a streaming surface and
	// must not let secret-shaped strings (commit messages, mail bodies,
	// any other 'any' payload) reach WS/SSE subscribers or sit in the
	// replay buffer raw.
	data := input.Data
	if h.redactor != nil && data != nil {
		data, _ = h.redactor.RedactStreamedEvent(data)
	}

	h.mu.Lock()
	ev := Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       input.Channel,
		Type:          input.Type,
		Sequence:      h.nextSequenceLocked(input.Channel),
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Actor:         input.Actor,
		CausationID:   input.CausationID,
		CorrelationID: input.CorrelationID,
		Data:          data,
	}
	h.events = append(h.events, ev)
	if len(h.events) > h.replayCapacity {
		copy(h.events, h.events[len(h.events)-h.replayCapacity:])
		h.events = h.events[:h.replayCapacity]
	}
	subs := make([]*subscriber, 0, len(h.subscribers))
	for _, sub := range h.subscribers {
		if sub.wants(ev.Channel) {
			subs = append(subs, sub)
		}
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.deliver(ev, h.lagEvent(ev))
	}
	return ev
}

func (h *EventHub) Replay(channel string, since uint64) ([]Event, bool) {
	window := h.replayWindow(channel, since)
	return window.Events, window.Gap
}

func (h *EventHub) Snapshot(channels []string, cursors map[string]uint64) Snapshot {
	snap, _ := h.SnapshotWithGaps(channels, cursors)
	return snap
}

func (h *EventHub) SnapshotWithGaps(channels []string, cursors map[string]uint64) (Snapshot, []Event) {
	if len(channels) == 0 {
		channels = []string{"_system"}
	}
	if cursors == nil {
		cursors = map[string]uint64{}
	}
	snap := Snapshot{
		SchemaVersion: schemaVersion,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Channels:      make(map[string]ChannelSnapshot, len(channels)),
	}
	gaps := make([]Event, 0)
	for _, channel := range channels {
		cursor := cursors[channel]
		window := h.replayWindow(channel, cursor)
		snap.Channels[channel] = ChannelSnapshot{
			LastSequence: window.LastSequence,
			Cursor:       cursor,
			Replayed:     window.Events,
			Gap:          window.Gap,
			Repair:       repairForGap(window.Gap),
		}
		if window.Gap {
			gaps = append(gaps, h.gapEvent(channel, cursor, window))
		}
	}
	return snap, gaps
}

func (h *EventHub) replayWindow(channel string, since uint64) replayWindow {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]Event, 0)
	var oldest uint64
	for _, ev := range h.events {
		if ev.Channel != channel {
			continue
		}
		if oldest == 0 || ev.Sequence < oldest {
			oldest = ev.Sequence
		}
		if ev.Sequence > since {
			out = append(out, ev)
		}
	}
	last := h.sequences[channel]
	gap := false
	switch {
	case since == 0:
	case oldest > 0:
		gap = since < oldest-1
	case last > since:
		gap = true
	}
	return replayWindow{
		Events:         out,
		Gap:            gap,
		OldestRetained: oldest,
		LastSequence:   last,
	}
}

func (h *EventHub) Heartbeat() Event {
	return h.transientEvent("_system", "heartbeat", map[string]any{"ok": true})
}

func (h *EventHub) CompatibilityWarning(clientSchemaVersion int) Event {
	return h.transientEvent("_system", "_compatibility_warning", map[string]any{
		"clientSchemaVersion": clientSchemaVersion,
		"serverSchemaVersion": schemaVersion,
	})
}

func (h *EventHub) Subscribe(ctx context.Context, channels []string) *Subscriber {
	if len(channels) == 0 {
		channels = []string{"_system"}
	}
	channelSet := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		channelSet[channel] = struct{}{}
	}

	h.mu.Lock()
	h.nextSubscriberID++
	sub := &subscriber{
		id:       h.nextSubscriberID,
		channels: channelSet,
		events:   make(chan Event, h.subscriberCapacity),
	}
	h.subscribers[sub.id] = sub
	h.mu.Unlock()

	wrapped := &Subscriber{hub: h, sub: sub}
	go func() {
		<-ctx.Done()
		wrapped.Close()
	}()
	return wrapped
}

func (s *Subscriber) Events() <-chan Event {
	return s.sub.events
}

func (s *Subscriber) Close() {
	s.hub.mu.Lock()
	if _, ok := s.hub.subscribers[s.sub.id]; ok {
		delete(s.hub.subscribers, s.sub.id)
	}
	s.hub.mu.Unlock()
}

func (s *subscriber) wants(channel string) bool {
	_, ok := s.channels[channel]
	return ok
}

func (s *subscriber) deliver(ev Event, lag Event) {
	select {
	case s.events <- ev:
		return
	default:
	}
	select {
	case <-s.events:
	default:
	}
	select {
	case s.events <- lag:
	default:
	}
}

func (h *EventHub) lagEvent(ev Event) Event {
	return Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       ev.Channel,
		Type:          "_lag",
		Sequence:      ev.Sequence,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Data: map[string]any{
			"lastPersistedOffset": ev.Sequence,
			"repair":              "replayEvents",
		},
	}
}

func (h *EventHub) nextSequenceLocked(channel string) uint64 {
	h.sequences[channel]++
	return h.sequences[channel]
}

func (h *EventHub) transientEvent(channel string, eventType string, data any) Event {
	h.mu.Lock()
	sequence := h.nextSequenceLocked(channel)
	h.mu.Unlock()
	return Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       channel,
		Type:          eventType,
		Sequence:      sequence,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Data:          data,
	}
}

type replayWindow struct {
	Events         []Event
	Gap            bool
	OldestRetained uint64
	LastSequence   uint64
}

func (h *EventHub) gapEvent(channel string, cursor uint64, window replayWindow) Event {
	from := cursor + 1
	to := window.LastSequence
	if window.OldestRetained > 0 {
		to = window.OldestRetained - 1
	}
	return Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       channel,
		Type:          "_gap",
		Sequence:      window.LastSequence,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Data: map[string]any{
			"from":           from,
			"to":             to,
			"repair":         "replayEvents",
			"oldestRetained": window.OldestRetained,
			"lastSequence":   window.LastSequence,
		},
	}
}

func repairForGap(gap bool) string {
	if gap {
		return "replayEvents"
	}
	return ""
}

func newEventID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "evt_unavailable"
	}
	return "evt_" + hex.EncodeToString(buf[:])
}
