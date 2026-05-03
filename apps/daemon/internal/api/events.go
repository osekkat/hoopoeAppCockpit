package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
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
}

type EventHub struct {
	mu sync.RWMutex

	now                func() time.Time
	replayCapacity     int
	subscriberCapacity int
	nextSequence       uint64
	events             []Event
	subscribers        map[uint64]*subscriber
	nextSubscriberID   uint64
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
		nextSequence:       1,
		events:             make([]Event, 0, replayCapacity),
		subscribers:        make(map[uint64]*subscriber),
	}
}

func (h *EventHub) Publish(input PublishInput) Event {
	if input.Channel == "" {
		input.Channel = "_system"
	}
	if input.Type == "" {
		input.Type = "daemon.event"
	}

	h.mu.Lock()
	ev := Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       input.Channel,
		Type:          input.Type,
		Sequence:      h.nextSequence,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Actor:         input.Actor,
		CausationID:   input.CausationID,
		CorrelationID: input.CorrelationID,
		Data:          input.Data,
	}
	h.nextSequence++
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
	gap := oldest > 0 && since > 0 && since < oldest-1
	return out, gap
}

func (h *EventHub) Snapshot(channels []string, cursors map[string]uint64) Snapshot {
	snap := Snapshot{
		SchemaVersion: schemaVersion,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Channels:      make(map[string]ChannelSnapshot, len(channels)),
	}
	for _, channel := range channels {
		cursor := cursors[channel]
		replayed, gap := h.Replay(channel, cursor)
		snap.Channels[channel] = ChannelSnapshot{
			LastSequence: h.lastSequence(channel),
			Cursor:       cursor,
			Replayed:     replayed,
			Gap:          gap,
			Repair:       repairForGap(gap),
		}
	}
	return snap
}

func (h *EventHub) Heartbeat() Event {
	h.mu.RLock()
	sequence := h.nextSequence - 1
	h.mu.RUnlock()
	return Event{
		EventID:       newEventID(),
		SchemaVersion: schemaVersion,
		Channel:       "_system",
		Type:          "heartbeat",
		Sequence:      sequence,
		Time:          h.now().UTC().Format(time.RFC3339Nano),
		Data:          map[string]any{"ok": true},
	}
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

func (h *EventHub) lastSequence(channel string) uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var last uint64
	for _, ev := range h.events {
		if ev.Channel == channel && ev.Sequence > last {
			last = ev.Sequence
		}
	}
	return last
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
