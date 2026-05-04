// Package clonesync keeps origin-mirror clones fresh from daemon Git events.
//
// It does not make the desktop mirror canonical. The package only runs
// read-only fetches against a configured clone path and derives a small VPS-WIP
// overlay from daemon events.
package clonesync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	gitevents "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/git"
)

const (
	DefaultPollInterval = 60 * time.Second
	DefaultFetchTimeout = 30 * time.Second
	DefaultHistoryLimit = 64

	TriggerOriginUpdated = "origin_updated"
	TriggerVPSPush       = "vps_push_completed"
	TriggerSafetyPoll    = "safety_poll"
	TriggerManualRefresh = "manual_refresh"
	TriggerReconnect     = "ws_reconnect"
)

var (
	ErrInvalidConfig   = errors.New("clone sync: invalid config")
	ErrUnknownProject  = errors.New("clone sync: unknown project")
	ErrProjectDisabled = errors.New("clone sync: project disabled")
)

type Project struct {
	ID        string `json:"id"`
	ClonePath string `json:"clonePath"`
	Remote    string `json:"remote,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type Config struct {
	Projects     []Project
	Fetcher      Fetcher
	EventSource  EventSource
	PollInterval time.Duration
	HistoryLimit int
	Now          func() time.Time
}

type Fetcher interface {
	Fetch(context.Context, Project, Trigger) (FetchResult, error)
}

type EventSource interface {
	Subscribe(context.Context, []string) Subscription
}

type Subscription interface {
	Events() <-chan api.Event
	Close()
}

type Trigger struct {
	Kind          string `json:"kind"`
	EventID       string `json:"eventId,omitempty"`
	EventType     string `json:"eventType,omitempty"`
	Sequence      uint64 `json:"sequence,omitempty"`
	CausationID   string `json:"causationId,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type FetchResult struct {
	Remote string `json:"remote,omitempty"`
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}

type FetchRecord struct {
	ProjectID     string        `json:"projectId"`
	ClonePath     string        `json:"clonePath"`
	Trigger       Trigger       `json:"trigger"`
	StartedAt     time.Time     `json:"startedAt"`
	CompletedAt   time.Time     `json:"completedAt"`
	Duration      time.Duration `json:"duration"`
	OK            bool          `json:"ok"`
	Error         string        `json:"error,omitempty"`
	Remote        string        `json:"remote,omitempty"`
	UnpushedCount int           `json:"unpushedCount"`
}

type State struct {
	ProjectID         string        `json:"projectId"`
	ClonePath         string        `json:"clonePath"`
	LastFetch         *FetchRecord  `json:"lastFetch,omitempty"`
	UnpushedCount     int           `json:"unpushedCount"`
	UnpushedCommits   []string      `json:"unpushedCommits,omitempty"`
	LastEventSequence uint64        `json:"lastEventSequence,omitempty"`
	LastEventID       string        `json:"lastEventId,omitempty"`
	History           []FetchRecord `json:"history,omitempty"`
}

type Service struct {
	mu           sync.Mutex
	projects     map[string]Project
	states       map[string]*projectState
	fetcher      Fetcher
	eventSource  EventSource
	pollInterval time.Duration
	historyLimit int
	now          func() time.Time
}

type projectState struct {
	lock              chan struct{}
	history           []FetchRecord
	lastFetch         *FetchRecord
	unpushed          map[string]struct{}
	lastEventID       string
	lastEventSequence uint64
}

type GitFetcher struct {
	Binary  string
	Timeout time.Duration
	Env     []string
}

func NewService(cfg Config) (*Service, error) {
	if cfg.Fetcher == nil {
		cfg.Fetcher = GitFetcher{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	historyLimit := cfg.HistoryLimit
	if historyLimit <= 0 {
		historyLimit = DefaultHistoryLimit
	}
	service := &Service{
		projects:     make(map[string]Project, len(cfg.Projects)),
		states:       make(map[string]*projectState, len(cfg.Projects)),
		fetcher:      cfg.Fetcher,
		eventSource:  cfg.EventSource,
		pollInterval: pollInterval,
		historyLimit: historyLimit,
		now:          now,
	}
	for _, project := range cfg.Projects {
		normalized, err := normalizeProject(project)
		if err != nil {
			return nil, err
		}
		service.projects[normalized.ID] = normalized
		service.states[normalized.ID] = &projectState{
			lock:     make(chan struct{}, 1),
			unpushed: make(map[string]struct{}),
		}
	}
	return service, nil
}

func (s *Service) Run(ctx context.Context) error {
	if s.eventSource == nil {
		return fmt.Errorf("%w: event source required", ErrInvalidConfig)
	}
	channels := s.channels()
	sub := s.eventSource.Subscribe(ctx, channels)
	defer sub.Close()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-sub.Events():
			if !ok {
				return nil
			}
			if _, err := s.HandleEvent(ctx, event); err != nil && !errors.Is(err, ErrUnknownProject) {
				return err
			}
		case <-ticker.C:
			if _, err := s.Poll(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *Service) HandleEvent(ctx context.Context, event api.Event) ([]FetchRecord, error) {
	projectID := projectIDFromEvent(event)
	if projectID == "" {
		return nil, nil
	}
	switch event.Type {
	case gitevents.EventVPSCommitCreated:
		commit := commitSHAFromEvent(event)
		s.recordUnpushed(projectID, commit, event)
		return nil, nil
	case gitevents.EventVPSPushCompleted:
		ok := pushOKFromEvent(event)
		s.recordPushed(projectID, pushedCommitsFromEvent(event), ok, event)
		if !ok {
			return nil, nil
		}
		record, err := s.fetch(ctx, projectID, triggerFromEvent(TriggerVPSPush, event))
		if err != nil {
			return []FetchRecord{record}, err
		}
		return []FetchRecord{record}, nil
	case gitevents.EventOriginUpdated:
		record, err := s.fetch(ctx, projectID, triggerFromEvent(TriggerOriginUpdated, event))
		if err != nil {
			return []FetchRecord{record}, err
		}
		return []FetchRecord{record}, nil
	default:
		return nil, nil
	}
}

func (s *Service) Refresh(ctx context.Context, projectID string) (FetchRecord, error) {
	return s.fetch(ctx, projectID, Trigger{Kind: TriggerManualRefresh, Reason: "on-demand refresh"})
}

func (s *Service) Reconnect(ctx context.Context, projectID string, lastSequence uint64) (FetchRecord, error) {
	return s.fetch(ctx, projectID, Trigger{Kind: TriggerReconnect, Sequence: lastSequence, Reason: "websocket reconnect"})
}

func (s *Service) Poll(ctx context.Context) ([]FetchRecord, error) {
	projects := s.projectIDs()
	records := make([]FetchRecord, 0, len(projects))
	for _, projectID := range projects {
		record, err := s.fetch(ctx, projectID, Trigger{Kind: TriggerSafetyPoll, Reason: "60s safety-net poll"})
		if err != nil {
			records = append(records, record)
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *Service) State(projectID string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, state, err := s.projectStateLocked(projectID)
	if err != nil {
		return State{}, err
	}
	return state.snapshot(project), nil
}

func (s *Service) States() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.projects))
	for id := range s.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]State, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.states[id].snapshot(s.projects[id]))
	}
	return out
}

func (s *Service) fetch(ctx context.Context, projectID string, trigger Trigger) (FetchRecord, error) {
	project, state, err := s.projectState(projectID)
	if err != nil {
		return FetchRecord{}, err
	}
	if project.Disabled {
		return FetchRecord{}, ErrProjectDisabled
	}
	if trigger.Kind == "" {
		trigger.Kind = TriggerManualRefresh
	}
	select {
	case state.lock <- struct{}{}:
	case <-ctx.Done():
		return FetchRecord{}, ctx.Err()
	}
	defer func() {
		<-state.lock
	}()

	started := s.now().UTC()
	result, fetchErr := s.fetcher.Fetch(ctx, project, trigger)
	completed := s.now().UTC()
	record := FetchRecord{
		ProjectID:   project.ID,
		ClonePath:   project.ClonePath,
		Trigger:     trigger,
		StartedAt:   started,
		CompletedAt: completed,
		Duration:    completed.Sub(started),
		OK:          fetchErr == nil,
		Remote:      nonEmpty(result.Remote, remoteForProject(project)),
	}
	if fetchErr != nil {
		record.Error = fetchErr.Error()
	}
	record = s.recordFetch(project.ID, record)
	if fetchErr != nil {
		return record, fetchErr
	}
	return record, nil
}

func (s *Service) recordUnpushed(projectID string, commit string, event api.Event) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[projectID]
	if state == nil {
		return
	}
	if commit != "" {
		state.unpushed[commit] = struct{}{}
	}
	state.lastEventID = event.EventID
	state.lastEventSequence = event.Sequence
}

func (s *Service) recordPushed(projectID string, commits []string, ok bool, event api.Event) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[projectID]
	if state == nil {
		return
	}
	if ok {
		if len(commits) == 0 {
			state.unpushed = map[string]struct{}{}
		} else {
			for _, commit := range commits {
				delete(state.unpushed, commit)
			}
		}
	}
	state.lastEventID = event.EventID
	state.lastEventSequence = event.Sequence
}

func (s *Service) recordFetch(projectID string, record FetchRecord) FetchRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := s.projects[projectID]
	state := s.states[projectID]
	if state == nil {
		return record
	}
	record.UnpushedCount = len(state.unpushed)
	copied := record
	state.lastFetch = &copied
	state.history = append(state.history, copied)
	if len(state.history) > s.historyLimit {
		state.history = append([]FetchRecord(nil), state.history[len(state.history)-s.historyLimit:]...)
	}
	state.lastEventID = nonEmpty(record.Trigger.EventID, state.lastEventID)
	if record.Trigger.Sequence > 0 {
		state.lastEventSequence = record.Trigger.Sequence
	}
	state.lastFetch.ClonePath = project.ClonePath
	return record
}

func (s *Service) projectState(projectID string) (Project, *projectState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectStateLocked(projectID)
}

func (s *Service) projectStateLocked(projectID string) (Project, *projectState, error) {
	projectID = strings.TrimSpace(projectID)
	project, ok := s.projects[projectID]
	if !ok {
		return Project{}, nil, fmt.Errorf("%w: %s", ErrUnknownProject, projectID)
	}
	state := s.states[projectID]
	if state == nil {
		state = &projectState{lock: make(chan struct{}, 1), unpushed: make(map[string]struct{})}
		s.states[projectID] = state
	}
	return project, state, nil
}

func (s *Service) channels() []string {
	ids := s.projectIDs()
	channels := make([]string, 0, len(ids))
	for _, id := range ids {
		channels = append(channels, gitevents.ProjectChannel(id))
	}
	return channels
}

func (s *Service) projectIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.projects))
	for id, project := range s.projects {
		if !project.Disabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (state *projectState) snapshot(project Project) State {
	commits := make([]string, 0, len(state.unpushed))
	for commit := range state.unpushed {
		commits = append(commits, commit)
	}
	sort.Strings(commits)
	var last *FetchRecord
	if state.lastFetch != nil {
		copied := *state.lastFetch
		last = &copied
	}
	return State{
		ProjectID:         project.ID,
		ClonePath:         project.ClonePath,
		LastFetch:         last,
		UnpushedCount:     len(commits),
		UnpushedCommits:   commits,
		LastEventSequence: state.lastEventSequence,
		LastEventID:       state.lastEventID,
		History:           append([]FetchRecord(nil), state.history...),
	}
}

func (f GitFetcher) Fetch(ctx context.Context, project Project, trigger Trigger) (FetchResult, error) {
	if strings.TrimSpace(project.ClonePath) == "" {
		return FetchResult{}, fmt.Errorf("%w: clone path required", ErrInvalidConfig)
	}
	binary := strings.TrimSpace(f.Binary)
	if binary == "" {
		binary = "git"
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = DefaultFetchTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-C", project.ClonePath, "-c", "color.ui=false", "fetch", "--all", "--tags", "--prune"}
	cmd := exec.CommandContext(runCtx, binary, args...)
	if f.Env != nil {
		cmd.Env = f.Env
	} else {
		cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := FetchResult{
		Remote: remoteForProject(project),
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		return result, fmt.Errorf("clone sync: %s fetch failed: %w (stderr: %s)", project.ID, err, truncate(stderr.String()))
	}
	return result, nil
}

func normalizeProject(project Project) (Project, error) {
	project.ID = strings.TrimSpace(project.ID)
	project.ClonePath = strings.TrimSpace(project.ClonePath)
	project.Remote = strings.TrimSpace(project.Remote)
	if project.Remote == "" {
		project.Remote = "origin"
	}
	if project.ID == "" {
		return Project{}, fmt.Errorf("%w: project id required", ErrInvalidConfig)
	}
	if project.ClonePath == "" {
		return Project{}, fmt.Errorf("%w: clone path required for %s", ErrInvalidConfig, project.ID)
	}
	return project, nil
}

func triggerFromEvent(kind string, event api.Event) Trigger {
	return Trigger{
		Kind:          kind,
		EventID:       event.EventID,
		EventType:     event.Type,
		Sequence:      event.Sequence,
		CausationID:   event.CausationID,
		CorrelationID: event.CorrelationID,
	}
}

func projectIDFromEvent(event api.Event) string {
	if projectID := projectIDFromData(event.Data); projectID != "" {
		return projectID
	}
	if strings.HasPrefix(event.Channel, "project:") {
		return strings.TrimPrefix(event.Channel, "project:")
	}
	return ""
}

func projectIDFromData(data any) string {
	switch value := data.(type) {
	case gitevents.CommitCreatedPayload:
		return strings.TrimSpace(value.ProjectID)
	case gitevents.PushCompletedPayload:
		return strings.TrimSpace(value.ProjectID)
	case gitevents.OriginUpdatedPayload:
		return strings.TrimSpace(value.ProjectID)
	case map[string]any:
		return stringField(value, "projectId")
	case map[string]string:
		return strings.TrimSpace(value["projectId"])
	default:
		return ""
	}
}

func commitSHAFromEvent(event api.Event) string {
	switch value := event.Data.(type) {
	case gitevents.CommitCreatedPayload:
		return strings.TrimSpace(value.CommitSHA)
	case map[string]any:
		return stringField(value, "commitSha")
	case map[string]string:
		return strings.TrimSpace(value["commitSha"])
	default:
		return ""
	}
}

func pushedCommitsFromEvent(event api.Event) []string {
	switch value := event.Data.(type) {
	case gitevents.PushCompletedPayload:
		return trimmedStrings(value.CommitsPushed)
	case map[string]any:
		return stringSliceField(value, "commitsPushed")
	case map[string]string:
		return trimmedStrings(strings.Split(value["commitsPushed"], ","))
	default:
		return nil
	}
}

func pushOKFromEvent(event api.Event) bool {
	switch value := event.Data.(type) {
	case gitevents.PushCompletedPayload:
		return value.OK
	case map[string]any:
		ok, _ := value["ok"].(bool)
		return ok
	case map[string]string:
		return value["ok"] == "true"
	default:
		return false
	}
}

func stringField(values map[string]any, key string) string {
	raw, _ := values[key].(string)
	return strings.TrimSpace(raw)
}

func stringSliceField(values map[string]any, key string) []string {
	raw, ok := values[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return trimmedStrings(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return trimmedStrings(out)
	case string:
		return trimmedStrings(strings.Split(value, ","))
	default:
		return nil
	}
}

func trimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func remoteForProject(project Project) string {
	if project.Remote == "" {
		return "origin"
	}
	return project.Remote
}

func nonEmpty(first string, fallback string) string {
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	return fallback
}

func truncate(value string) string {
	const limit = 512
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}
