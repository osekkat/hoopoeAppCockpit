// Package activity turns canonical tool events into Activity timeline events.
package activity

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/agentmail"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
)

const (
	EventMailSent                = "mail.sent"
	EventMailReceived            = "mail.received"
	EventMailUrgent              = "mail.urgent"
	EventReservationRequested    = "reservation.requested"
	EventReservationRenewed      = "reservation.renewed"
	EventReservationReleased     = "reservation.released"
	EventReservationConflicted   = "reservation.conflicted"
	DefaultPollInterval          = 15 * time.Second
	defaultReservationFetchLimit = 500
)

var ErrInvalidConfig = errors.New("activity: invalid config")

type AgentMailClient interface {
	FetchInbox(context.Context, agentmail.FetchInboxRequest) ([]agentmail.Message, error)
	ListReservations(context.Context, agentmail.ListReservationsRequest) ([]agentmail.Reservation, error)
	ForceReleaseReservation(context.Context, agentmail.ForceReleaseReservationRequest) (agentmail.ForceReleaseReservationResponse, error)
}

type Publisher interface {
	Publish(api.PublishInput) api.Event
}

type Config struct {
	ProjectID     string
	ProjectKey    string
	AgentName     string
	Mail          AgentMailClient
	Events        Publisher
	Now           func() time.Time
	PollInterval  time.Duration
	IncludeBodies bool
}

type Ingestor struct {
	mu sync.Mutex

	projectID     string
	projectKey    string
	agentName     string
	channel       string
	mail          AgentMailClient
	events        Publisher
	now           func() time.Time
	pollInterval  time.Duration
	includeBodies bool

	seenMessages map[int]struct{}
	reservations map[int]reservationState
	conflicts    map[string]struct{}
}

type ActivityData struct {
	Kind          string               `json:"kind"`
	Category      string               `json:"category"`
	Importance    string               `json:"importance"`
	Summary       string               `json:"summary"`
	Timestamp     string               `json:"timestamp"`
	Actor         ActivityActor        `json:"actor"`
	Pills         []ActivityPill       `json:"pills,omitempty"`
	InlinePreview string               `json:"inlinePreview,omitempty"`
	Pivot         *ActivityPivot       `json:"pivot,omitempty"`
	Source        string               `json:"source"`
	ProjectID     string               `json:"projectId"`
	BeadID        string               `json:"beadId,omitempty"`
	Mail          *MailData            `json:"mail,omitempty"`
	Reservation   *ReservationData     `json:"reservation,omitempty"`
	Conflict      *ReservationConflict `json:"conflict,omitempty"`
	Extra         map[string]any       `json:"extra,omitempty"`
}

type ActivityActor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
}

type ActivityPill struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tone  string `json:"tone,omitempty"`
}

type ActivityPivot struct {
	Kind      string `json:"kind"`
	BeadID    string `json:"beadId,omitempty"`
	ProjectID string `json:"projectId"`
	Path      string `json:"path,omitempty"`
}

type MailData struct {
	MessageID   int      `json:"messageId"`
	ThreadID    string   `json:"threadId,omitempty"`
	Subject     string   `json:"subject"`
	From        string   `json:"from"`
	To          []string `json:"to,omitempty"`
	CC          []string `json:"cc,omitempty"`
	Importance  string   `json:"importance"`
	AckRequired bool     `json:"ackRequired"`
	CreatedTS   string   `json:"createdTs"`
}

type ReservationData struct {
	ID         int      `json:"id"`
	Agent      string   `json:"agent"`
	Paths      []string `json:"paths"`
	Exclusive  bool     `json:"exclusive"`
	Reason     string   `json:"reason,omitempty"`
	CreatedTS  string   `json:"createdTs,omitempty"`
	ExpiresTS  string   `json:"expiresTs,omitempty"`
	ReleasedTS string   `json:"releasedTs,omitempty"`
}

type ReservationConflict struct {
	Type        string            `json:"type"`
	Reservation ReservationData   `json:"reservation"`
	Other       *ReservationData  `json:"other,omitempty"`
	Action      map[string]string `json:"action,omitempty"`
}

type reservationState struct {
	Reservation agentmail.Reservation
	Active      bool
}

func NewAgentMailIngestor(cfg Config) (*Ingestor, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return nil, fmt.Errorf("%w: project id is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.ProjectKey) == "" {
		return nil, fmt.Errorf("%w: project key is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.AgentName) == "" {
		return nil, fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
	}
	if cfg.Mail == nil {
		return nil, fmt.Errorf("%w: Agent Mail client is required", ErrInvalidConfig)
	}
	if cfg.Events == nil {
		return nil, fmt.Errorf("%w: event publisher is required", ErrInvalidConfig)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	channel, err := ActivityChannel(cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	return &Ingestor{
		projectID:     cfg.ProjectID,
		projectKey:    cfg.ProjectKey,
		agentName:     cfg.AgentName,
		channel:       channel,
		mail:          cfg.Mail,
		events:        cfg.Events,
		now:           now,
		pollInterval:  pollInterval,
		includeBodies: cfg.IncludeBodies,
		seenMessages:  make(map[int]struct{}),
		reservations:  make(map[int]reservationState),
		conflicts:     make(map[string]struct{}),
	}, nil
}

func ActivityChannel(projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", fmt.Errorf("%w: project id is required", ErrInvalidConfig)
	}
	if !safeToken(projectID) {
		return "", fmt.Errorf("%w: unsafe project id %q", ErrInvalidConfig, projectID)
	}
	return "activity:" + projectID, nil
}

func (i *Ingestor) SyncOnce(ctx context.Context) ([]api.Event, error) {
	messages, err := i.mail.FetchInbox(ctx, agentmail.FetchInboxRequest{
		ProjectKey:    i.projectKey,
		AgentName:     i.agentName,
		Limit:         50,
		IncludeBodies: i.includeBodies,
	})
	if err != nil {
		return nil, err
	}
	reservations, err := i.mail.ListReservations(ctx, agentmail.ListReservationsRequest{
		Project:    i.projectKey,
		ActiveOnly: false,
		Limit:      defaultReservationFetchLimit,
	})
	if err != nil {
		return nil, err
	}
	events := i.IngestMessages(messages)
	events = append(events, i.IngestReservations(reservations)...)
	return events, nil
}

func (i *Ingestor) Run(ctx context.Context) error {
	if _, err := i.SyncOnce(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(i.pollInterval)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if _, err := i.SyncOnce(ctx); err != nil {
				return err
			}
			timer.Reset(i.pollInterval)
		}
	}
}

func (i *Ingestor) IngestMessages(messages []agentmail.Message) []api.Event {
	i.mu.Lock()
	defer i.mu.Unlock()

	events := make([]api.Event, 0, len(messages))
	for _, msg := range messages {
		if msg.ID <= 0 {
			continue
		}
		if _, seen := i.seenMessages[msg.ID]; seen {
			continue
		}
		i.seenMessages[msg.ID] = struct{}{}
		events = append(events, i.publishMessageLocked(msg))
	}
	return events
}

func (i *Ingestor) IngestReservations(reservations []agentmail.Reservation) []api.Event {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now().UTC()
	events := make([]api.Event, 0, len(reservations))
	current := make(map[int]agentmail.Reservation, len(reservations))
	active := make([]agentmail.Reservation, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.ID <= 0 {
			continue
		}
		current[reservation.ID] = reservation
		isActive := reservation.ReleasedTS == ""
		if isActive {
			active = append(active, reservation)
		}
		previous, hadPrevious := i.reservations[reservation.ID]
		i.reservations[reservation.ID] = reservationState{Reservation: reservation, Active: isActive}
		switch {
		case !hadPrevious && isActive:
			events = append(events, i.publishReservationLocked(EventReservationRequested, reservation, "reserved", nil))
		case hadPrevious && previous.Active && !isActive:
			events = append(events, i.publishReservationLocked(EventReservationReleased, reservation, "released", nil))
		case hadPrevious && previous.Active && isActive && previous.Reservation.ExpiresTS != reservation.ExpiresTS:
			events = append(events, i.publishReservationLocked(EventReservationRenewed, reservation, "renewed", nil))
		}
		if isActive && expired(reservation.ExpiresTS, now) {
			key := fmt.Sprintf("stale:%d:%s", reservation.ID, reservation.ExpiresTS)
			if _, seen := i.conflicts[key]; !seen {
				i.conflicts[key] = struct{}{}
				conflict := &ReservationConflict{
					Type:        "stale",
					Reservation: reservationData(reservation),
					Action: map[string]string{
						"kind": "reservation.force_release",
						"id":   fmt.Sprintf("%d", reservation.ID),
					},
				}
				events = append(events, i.publishReservationLocked(EventReservationConflicted, reservation, "stale", conflict))
			}
		}
	}
	for id, previous := range i.reservations {
		if _, ok := current[id]; ok || !previous.Active {
			continue
		}
		released := previous.Reservation
		released.ReleasedTS = now.Format(time.RFC3339Nano)
		i.reservations[id] = reservationState{Reservation: released, Active: false}
		events = append(events, i.publishReservationLocked(EventReservationReleased, released, "released", nil))
	}
	for _, conflict := range DetectReservationConflicts(active) {
		key := fmt.Sprintf("overlap:%d:%d", conflict.Reservation.ID, conflict.Other.ID)
		if _, seen := i.conflicts[key]; seen {
			continue
		}
		i.conflicts[key] = struct{}{}
		events = append(events, i.publishReservationLocked(EventReservationConflicted, toAgentMailReservation(conflict.Reservation), "conflict", &ReservationConflict{
			Type:        "overlap",
			Reservation: reservationData(toAgentMailReservation(conflict.Reservation)),
			Other:       ptrReservationData(toAgentMailReservation(conflict.Other)),
			Action: map[string]string{
				"kind": "reservation.force_release",
				"id":   fmt.Sprintf("%d", conflict.Reservation.ID),
			},
		}))
	}
	return events
}

func (i *Ingestor) ForceReleaseReservation(ctx context.Context, req agentmail.ForceReleaseReservationRequest) (agentmail.ForceReleaseReservationResponse, api.Event, error) {
	if strings.TrimSpace(req.ProjectKey) == "" {
		req.ProjectKey = i.projectKey
	}
	if strings.TrimSpace(req.AgentName) == "" {
		req.AgentName = i.agentName
	}
	out, err := i.mail.ForceReleaseReservation(ctx, req)
	reservation := agentmail.Reservation{
		ID:         req.FileReservationID,
		Agent:      req.AgentName,
		Reason:     req.Note,
		ReleasedTS: i.now().UTC().Format(time.RFC3339Nano),
	}
	if err != nil {
		event := i.publishReservationLocked(EventReservationConflicted, reservation, "force_release_failed", &ReservationConflict{
			Type:        "force_release_failed",
			Reservation: reservationData(reservation),
			Action: map[string]string{
				"kind":  "reservation.force_release",
				"id":    fmt.Sprintf("%d", req.FileReservationID),
				"error": err.Error(),
			},
		})
		return out, event, err
	}
	event := i.publishReservationLocked(EventReservationReleased, reservation, "force_released", nil)
	return out, event, nil
}

func (i *Ingestor) publishMessageLocked(msg agentmail.Message) api.Event {
	kind := messageKind(msg, i.agentName)
	beadID := beadIDFromThread(msg.ThreadID)
	importance := "info"
	if kind == EventMailUrgent {
		importance = "urgent"
	}
	data := ActivityData{
		Kind:          kind,
		Category:      "mail",
		Importance:    importance,
		Summary:       messageSummary(kind, msg),
		Timestamp:     firstNonEmpty(msg.CreatedTS, i.now().UTC().Format(time.RFC3339Nano)),
		Actor:         actor(msg.From),
		InlinePreview: preview(msg.BodyMD),
		Source:        "agent_mail",
		ProjectID:     i.projectID,
		BeadID:        beadID,
		Mail: &MailData{
			MessageID:   msg.ID,
			ThreadID:    msg.ThreadID,
			Subject:     msg.Subject,
			From:        msg.From,
			To:          cloneStrings(msg.To),
			CC:          cloneStrings(msg.CC),
			Importance:  msg.Importance,
			AckRequired: msg.AckRequired,
			CreatedTS:   msg.CreatedTS,
		},
	}
	if beadID != "" {
		data.Pills = append(data.Pills, ActivityPill{ID: "bead", Label: beadID})
		data.Pivot = &ActivityPivot{Kind: "bead", BeadID: beadID, ProjectID: i.projectID}
	}
	if msg.AckRequired {
		data.Pills = append(data.Pills, ActivityPill{ID: "ack", Label: "ack required", Tone: "warn"})
	}
	return i.events.Publish(api.PublishInput{
		Channel: i.channel,
		Type:    kind,
		Actor: map[string]any{
			"kind": "agent",
			"id":   firstNonEmpty(msg.From, "agent_mail"),
		},
		CorrelationID: msg.ThreadID,
		Data:          data,
	})
}

func (i *Ingestor) publishReservationLocked(kind string, reservation agentmail.Reservation, reason string, conflict *ReservationConflict) api.Event {
	importance := "info"
	if kind == EventReservationConflicted {
		importance = "urgent"
	}
	data := ActivityData{
		Kind:        kind,
		Category:    "reservations",
		Importance:  importance,
		Summary:     reservationSummary(kind, reservation, reason, conflict),
		Timestamp:   i.now().UTC().Format(time.RFC3339Nano),
		Actor:       actor(firstNonEmpty(reservation.Agent, "agent_mail")),
		Source:      "agent_mail",
		ProjectID:   i.projectID,
		BeadID:      beadIDFromThread(reservation.Reason),
		Reservation: ptrReservationData(reservation),
		Conflict:    conflict,
	}
	if data.BeadID != "" {
		data.Pills = append(data.Pills, ActivityPill{ID: "bead", Label: data.BeadID})
	}
	if reservation.PathPattern != "" {
		data.Pills = append(data.Pills, ActivityPill{ID: "path", Label: reservation.PathPattern})
		data.Pivot = &ActivityPivot{Kind: "reservation", ProjectID: i.projectID, Path: reservation.PathPattern}
	}
	return i.events.Publish(api.PublishInput{
		Channel: i.channel,
		Type:    kind,
		Actor: map[string]any{
			"kind": "agent",
			"id":   firstNonEmpty(reservation.Agent, "agent_mail"),
		},
		CorrelationID: data.BeadID,
		Data:          data,
	})
}

func messageKind(msg agentmail.Message, localAgent string) string {
	if strings.EqualFold(msg.Importance, "urgent") {
		return EventMailUrgent
	}
	if msg.AckRequired {
		return EventMailUrgent
	}
	if strings.EqualFold(strings.TrimSpace(msg.From), strings.TrimSpace(localAgent)) {
		return EventMailSent
	}
	return EventMailReceived
}

func messageSummary(kind string, msg agentmail.Message) string {
	subject := firstNonEmpty(strings.TrimSpace(msg.Subject), "(no subject)")
	switch kind {
	case EventMailSent:
		return fmt.Sprintf("Outbound to %s - %s", joinNames(msg.To), subject)
	case EventMailUrgent:
		return "URGENT: " + subject
	default:
		return fmt.Sprintf("Mail from %s - %s", firstNonEmpty(msg.From, "unknown"), subject)
	}
}

func reservationSummary(kind string, reservation agentmail.Reservation, reason string, conflict *ReservationConflict) string {
	target := firstNonEmpty(reservation.PathPattern, fmt.Sprintf("reservation %d", reservation.ID))
	switch kind {
	case EventReservationRenewed:
		return "Renewed " + target
	case EventReservationReleased:
		return "Released " + target
	case EventReservationConflicted:
		if conflict != nil && conflict.Type == "stale" {
			return fmt.Sprintf("STALE: %s held by %s", target, firstNonEmpty(reservation.Agent, "unknown"))
		}
		if conflict != nil && conflict.Other != nil {
			return fmt.Sprintf("CONFLICT: %s held by %s and %s", target, firstNonEmpty(reservation.Agent, "unknown"), firstNonEmpty(conflict.Other.Agent, "unknown"))
		}
		return "CONFLICT: " + target
	default:
		if reason == "force_released" {
			return "Force-released " + target
		}
		return "Reserved " + target
	}
}

type ReservationOverlap struct {
	Reservation ReservationData `json:"reservation"`
	Other       ReservationData `json:"other"`
}

func DetectReservationConflicts(reservations []agentmail.Reservation) []ReservationOverlap {
	out := make([]ReservationOverlap, 0)
	for i := 0; i < len(reservations); i++ {
		left := reservations[i]
		if left.ReleasedTS != "" {
			continue
		}
		for j := i + 1; j < len(reservations); j++ {
			right := reservations[j]
			if right.ReleasedTS != "" || sameAgent(left.Agent, right.Agent) {
				continue
			}
			if !left.Exclusive && !right.Exclusive {
				continue
			}
			if patternsOverlap(left.PathPattern, right.PathPattern) {
				out = append(out, ReservationOverlap{
					Reservation: reservationData(left),
					Other:       reservationData(right),
				})
			}
		}
	}
	return out
}

func patternsOverlap(left string, right string) bool {
	a := patternScopeOf(left)
	b := patternScopeOf(right)
	if a.prefix == "" || b.prefix == "" {
		return false
	}
	if a.exact && b.exact {
		return a.prefix == b.prefix
	}
	if a.exact {
		return pathWithin(a.prefix, b.prefix)
	}
	if b.exact {
		return pathWithin(b.prefix, a.prefix)
	}
	return pathWithin(a.prefix, b.prefix) || pathWithin(b.prefix, a.prefix)
}

type patternScope struct {
	prefix string
	exact  bool
}

func patternScopeOf(pattern string) patternScope {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	if pattern == "" {
		return patternScope{}
	}
	meta := strings.IndexAny(pattern, "*?[{")
	if meta < 0 {
		return patternScope{prefix: cleanPath(pattern), exact: true}
	}
	prefix := pattern[:meta]
	if !strings.HasSuffix(prefix, "/") {
		prefix = path.Dir(prefix)
	}
	return patternScope{prefix: cleanPath(prefix), exact: false}
}

func pathWithin(child string, parent string) bool {
	child = cleanPath(child)
	parent = cleanPath(parent)
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func cleanPath(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" || value == "." {
		return ""
	}
	return strings.Trim(path.Clean(value), "/")
}

func reservationData(reservation agentmail.Reservation) ReservationData {
	paths := []string{}
	if reservation.PathPattern != "" {
		paths = []string{reservation.PathPattern}
	}
	return ReservationData{
		ID:         reservation.ID,
		Agent:      reservation.Agent,
		Paths:      paths,
		Exclusive:  reservation.Exclusive,
		Reason:     reservation.Reason,
		CreatedTS:  reservation.CreatedTS,
		ExpiresTS:  reservation.ExpiresTS,
		ReleasedTS: reservation.ReleasedTS,
	}
}

func ptrReservationData(reservation agentmail.Reservation) *ReservationData {
	data := reservationData(reservation)
	return &data
}

func toAgentMailReservation(data ReservationData) agentmail.Reservation {
	reservation := agentmail.Reservation{
		ID:         data.ID,
		Agent:      data.Agent,
		Exclusive:  data.Exclusive,
		Reason:     data.Reason,
		CreatedTS:  data.CreatedTS,
		ExpiresTS:  data.ExpiresTS,
		ReleasedTS: data.ReleasedTS,
	}
	if len(data.Paths) > 0 {
		reservation.PathPattern = data.Paths[0]
	}
	return reservation
}

func actor(id string) ActivityActor {
	id = firstNonEmpty(strings.TrimSpace(id), "agent_mail")
	return ActivityActor{ID: id, DisplayName: id, Kind: "agent"}
}

func beadIDFromThread(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if strings.HasPrefix(threadID, "br-") && len(threadID) > len("br-") {
		return strings.TrimPrefix(threadID, "br-")
	}
	if strings.HasPrefix(threadID, "hp-") {
		return threadID
	}
	return ""
}

func expired(raw string, now time.Time) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return false
		}
	}
	return ts.Before(now) || ts.Equal(now)
}

func safeToken(value string) bool {
	if len(value) > 96 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func sameAgent(a string, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func joinNames(values []string) string {
	if len(values) == 0 {
		return "recipient"
	}
	return strings.Join(values, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func preview(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 240 {
		return value
	}
	return value[:240] + "..."
}
