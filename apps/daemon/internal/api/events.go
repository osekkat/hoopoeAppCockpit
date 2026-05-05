package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const (
	defaultReplayCapacity     = 1024
	defaultSubscriberCapacity = 64

	// EventTypeEncodeError is the sentinel Type assigned to events whose
	// original Data could not be JSON-marshaled. The sentinel preserves
	// the channel, sequence, timestamp, and originating event metadata
	// so subscribers see a meaningful skip marker instead of a phantom
	// gap (writeSSE used to silently drop them) or a torn-down WS
	// connection (writeWebSocketJSON used to return the marshal error,
	// which closed the socket). hp-4qbg.
	EventTypeEncodeError = "_encode_error"
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
	hub         *EventHub
	sub         *subscriber
	done        chan struct{}
	watcherDone chan struct{}
	closeOnce   sync.Once
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
	data := h.redactData(input.Data)

	// hp-4qbg: pre-validate that the Data + Actor (the two `any` fields
	// on Event that producers control) actually marshal. If they don't,
	// poison the replay buffer / silently drop SSE / tear down WS.
	// Substitute a marshalable sentinel and rewrite Type to the
	// EventTypeEncodeError marker. The sequence number assigned below
	// stays correct so subscribers see a clean monotonic stream.
	originalType := input.Type
	dataMarshalErr := tryMarshal(data)
	actorMarshalErr := tryMarshal(input.Actor)
	if dataMarshalErr != nil || actorMarshalErr != nil {
		input.Type = EventTypeEncodeError
		sentinel := map[string]any{
			"_encodeError":  true,
			"originalType":  originalType,
		}
		if dataMarshalErr != nil {
			sentinel["dataMarshalError"] = dataMarshalErr.Error()
		}
		if actorMarshalErr != nil {
			sentinel["actorMarshalError"] = actorMarshalErr.Error()
		}
		data = sentinel
		input.Actor = nil
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

// redactData scrubs Publish.Data through the configured redactor before the
// event is persisted in the replay buffer or delivered to subscribers.
// redaction.RedactValue walks typed structs by emitting a map[string]any
// keyed by JSON tag. Producer payloads like gitevents.CommitCreatedPayload
// and activity.ActivityData need their typed Go shape preserved so existing
// callers, tests, and replay-buffer consumers keep the expected struct
// identity — re-decode the redacted map back into the source type via JSON.
func (h *EventHub) redactData(value any) any {
	if h.redactor == nil || value == nil {
		return value
	}
	redacted, _ := h.redactor.RedactStreamedEvent(value)
	rt := reflect.TypeOf(value)
	if rt == nil {
		return redacted
	}
	if !isStructLike(rt) {
		return redacted
	}
	redactedMap, ok := redacted.(map[string]any)
	if !ok {
		return redacted
	}
	body, err := json.Marshal(redactedMap)
	if err != nil {
		return redacted
	}
	target := reflect.New(rt)
	if err := json.Unmarshal(body, target.Interface()); err != nil {
		return redacted
	}
	return target.Elem().Interface()
}

func isStructLike(rt reflect.Type) bool {
	switch rt.Kind() {
	case reflect.Struct:
		return true
	case reflect.Pointer:
		return rt.Elem().Kind() == reflect.Struct
	}
	return false
}

// tryMarshal returns nil if the value JSON-marshals cleanly, or the
// underlying marshal error otherwise. Used by Publish (hp-4qbg) to detect
// chan/func/unsafe.Pointer Data and Actor fields BEFORE they hit the
// replay buffer, where they would later silently drop on SSE or tear
// down a WS connection.
func tryMarshal(value any) error {
	if value == nil {
		return nil
	}
	if _, err := json.Marshal(value); err != nil {
		return err
	}
	return nil
}

// LastSequence returns the highest sequence number ever assigned for a
// channel. Used by the replay/SSE/WS handlers to reject cursors that
// claim to have seen sequences the daemon never produced — a
// forward-time-travel state that violates the per-channel monotonic
// invariant (hp-0vkn).
func (h *EventHub) LastSequence(channel string) uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sequences[channel]
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
	// Reject nil ctx by substituting context.Background — the watcher
	// goroutine below selects on ctx.Done(), and a nil ctx would panic
	// the goroutine on the very first select. Mirrors the WaitContext
	// pattern in scheduler.go.
	if ctx == nil {
		ctx = context.Background()
	}
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

	wrapped := &Subscriber{
		hub:         h,
		sub:         sub,
		done:        make(chan struct{}),
		watcherDone: make(chan struct{}),
	}
	go func() {
		defer close(wrapped.watcherDone)
		// Mirror scheduler.recoverDispatch: a daemon-level goroutine
		// must never be able to take the process down. If anything
		// panics inside the select (a future ctx that violates the
		// context.Context contract, an exotic cancel propagation, or
		// any later code added here), the recover absorbs it and the
		// subscriber is best-effort closed so the EventHub map doesn't
		// leak the entry.
		defer func() {
			if r := recover(); r != nil {
				wrapped.Close()
			}
		}()
		select {
		case <-ctx.Done():
			wrapped.Close()
		case <-wrapped.done:
		}
	}()
	return wrapped
}

func (s *Subscriber) Events() <-chan Event {
	return s.sub.events
}

func (s *Subscriber) Close() {
	s.closeOnce.Do(func() {
		func() {
			s.hub.mu.Lock()
			defer s.hub.mu.Unlock()
			delete(s.hub.subscribers, s.sub.id)
		}()
		close(s.done)
	})
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

// transientEvent constructs a control-plane event (heartbeat,
// compatibility warning, lag/gap markers) that is sent directly to a
// single SSE/WS connection rather than appended to the replay buffer
// or fan-routed via subscribers.deliver. hp-2wrg: previous versions
// called nextSequenceLocked (mutating the channel cursor), so per-
// connection heartbeat timers burned _system sequence numbers. After
// N heartbeats, h.sequences['_system'] was N, and the next REAL
// _system event arrived at seq=N+1 — a Subscribe('_system') at
// cursor=0 saw last>since with an empty replay buffer (heartbeats
// aren't appended) and reported a phantom gap.
//
// Read the channel's current sequence WITHOUT mutating it. Receivers
// that care about ordering still see a meaningful sequence; the next
// real Publish on that channel still gets sequence currentMax+1.
func (h *EventHub) transientEvent(channel string, eventType string, data any) Event {
	h.mu.RLock()
	sequence := h.sequences[channel]
	h.mu.RUnlock()
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
