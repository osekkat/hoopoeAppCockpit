package acfs

import (
	"errors"
	"fmt"
	"time"
)

const (
	DefaultLowConfidenceAfterLines = 12
	DefaultLastLineLimit           = 20
	DefaultResumeHint              = "/v1/bootstrap/acfs/resume"
)

var ErrInvalidParser = errors.New("acfs: invalid parser")

type ParserConfig struct {
	RunID                   string
	Markers                 MarkerLibrary
	LowConfidenceAfterLines int
	LastLineLimit           int
	ResumeHint              string
	Now                     func() time.Time
}

type Parser struct {
	runID                   string
	markers                 MarkerLibrary
	lowConfidenceAfterLines int
	lastLineLimit           int
	resumeHint              string
	now                     func() time.Time
	confidence              Confidence
	rawLogFallback          bool
	current                 *PhaseState
	completed               []PhaseState
	lastLines               []string
	linesSinceMarker        int
	lastOffset              int64
	finished                bool
	sawFailure              bool
}

func NewParser(cfg ParserConfig) *Parser {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	lowAfter := cfg.LowConfidenceAfterLines
	if lowAfter <= 0 {
		lowAfter = DefaultLowConfidenceAfterLines
	}
	lastLineLimit := cfg.LastLineLimit
	if lastLineLimit <= 0 {
		lastLineLimit = DefaultLastLineLimit
	}
	resumeHint := cfg.ResumeHint
	if resumeHint == "" {
		resumeHint = DefaultResumeHint
	}
	markers := cfg.Markers
	if markers.Ref == "" {
		markers = DefaultMarkerLibrary(DefaultPinnedRef)
	}
	return &Parser{
		runID:                   cfg.RunID,
		markers:                 markers,
		lowConfidenceAfterLines: lowAfter,
		lastLineLimit:           lastLineLimit,
		resumeHint:              resumeHint,
		now:                     now,
		confidence:              ConfidenceHigh,
	}
}

func (p *Parser) Observe(line Line) ([]Event, error) {
	if p == nil {
		return nil, ErrInvalidParser
	}
	if p.finished {
		return nil, fmt.Errorf("%w: parser already finished", ErrInvalidParser)
	}
	if line.At.IsZero() {
		line.At = p.now().UTC()
	}
	p.lastOffset = line.Offset
	p.rememberLine(line.Text)
	events := []Event{p.event(Event{
		Type:   EventPhaseLine,
		Phase:  p.currentPhase(),
		Stream: line.Stream,
		Offset: line.Offset,
		Text:   line.Text,
		At:     line.At.UTC(),
	})}

	m, ok := p.markers.Parse(line.Text)
	if !ok {
		p.linesSinceMarker++
		if !p.rawLogFallback && p.linesSinceMarker >= p.lowConfidenceAfterLines {
			p.confidence = ConfidenceLow
			p.rawLogFallback = true
			events = append(events, p.event(Event{
				Type:           EventParserConfidence,
				Confidence:     ConfidenceLow,
				RawLogFallback: true,
				ResumeHint:     p.resumeHint,
				At:             line.At.UTC(),
			}))
		}
		return events, nil
	}
	p.linesSinceMarker = 0
	switch m.kind {
	case markerStart:
		events = append(events, p.startPhase(m, line.At.UTC()))
	case markerCheckpoint:
		events = append(events, p.checkpoint(m, line.At.UTC()))
	case markerEnd:
		events = append(events, p.endPhase(m, line.At.UTC()))
	case markerFail:
		events = append(events, p.failPhase(m, line.At.UTC()))
	}
	return events, nil
}

func (p *Parser) Finish(rc int, at time.Time) ([]Event, error) {
	if p == nil {
		return nil, ErrInvalidParser
	}
	if p.finished {
		return nil, fmt.Errorf("%w: parser already finished", ErrInvalidParser)
	}
	p.finished = true
	if at.IsZero() {
		at = p.now().UTC()
	}
	if rc == 0 {
		if p.current == nil {
			return nil, nil
		}
		return []Event{p.endPhase(marker{kind: markerEnd, phase: p.current.Phase, rc: 0}, at.UTC())}, nil
	}
	if p.sawFailure {
		return nil, nil
	}
	phase := "bootstrap"
	if p.current != nil && p.current.Phase != "" {
		phase = p.current.Phase
	}
	return []Event{p.event(Event{
		Type:       EventPhaseFail,
		Phase:      phase,
		RC:         rc,
		LastLines:  append([]string(nil), p.lastLines...),
		ResumeHint: p.resumeHint,
		At:         at.UTC(),
	})}, nil
}

func (p *Parser) State() ParserState {
	if p == nil {
		return ParserState{}
	}
	completed := append([]PhaseState(nil), p.completed...)
	return ParserState{
		RunID:          p.runID,
		CurrentPhase:   p.currentPhase(),
		Completed:      completed,
		Confidence:     p.confidence,
		RawLogFallback: p.rawLogFallback,
		LastOffset:     p.lastOffset,
		ResumeHint:     p.resumeHint,
	}
}

func (p *Parser) startPhase(m marker, at time.Time) Event {
	if p.current != nil && p.current.Phase != m.phase {
		p.completed = append(p.completed, *p.current)
	}
	p.current = &PhaseState{
		Phase:       m.phase,
		Name:        firstNonEmpty(m.name, m.phase),
		StartedAt:   at,
		Checkpoints: map[string]CheckpointStatus{},
	}
	return p.event(Event{
		Type:  EventPhaseStart,
		Phase: p.current.Phase,
		Name:  p.current.Name,
		At:    at,
	})
}

func (p *Parser) checkpoint(m marker, at time.Time) Event {
	if p.current == nil || p.current.Phase != m.phase {
		p.current = &PhaseState{
			Phase:       m.phase,
			Name:        m.phase,
			StartedAt:   at,
			Checkpoints: map[string]CheckpointStatus{},
		}
	}
	p.current.Checkpoints[m.key] = m.status
	return p.event(Event{
		Type:   EventPhaseCheckpoint,
		Phase:  m.phase,
		Key:    m.key,
		Status: m.status,
		At:     at,
	})
}

func (p *Parser) endPhase(m marker, at time.Time) Event {
	state := p.current
	if state == nil || state.Phase != m.phase {
		state = &PhaseState{
			Phase:       m.phase,
			Name:        m.phase,
			StartedAt:   at,
			Checkpoints: map[string]CheckpointStatus{},
		}
	}
	state.EndedAt = at
	state.RC = m.rc
	if m.durationMs > 0 {
		state.DurationMs = m.durationMs
	} else if !state.StartedAt.IsZero() {
		state.DurationMs = at.Sub(state.StartedAt).Milliseconds()
	}
	p.completed = append(p.completed, *state)
	if p.current != nil && p.current.Phase == m.phase {
		p.current = nil
	}
	return p.event(Event{
		Type:       EventPhaseEnd,
		Phase:      state.Phase,
		RC:         state.RC,
		DurationMs: state.DurationMs,
		At:         at,
	})
}

func (p *Parser) failPhase(m marker, at time.Time) Event {
	if p.current == nil || p.current.Phase != m.phase {
		p.current = &PhaseState{Phase: m.phase, Name: m.phase, StartedAt: at}
	}
	p.sawFailure = true
	return p.event(Event{
		Type:       EventPhaseFail,
		Phase:      m.phase,
		RC:         m.rc,
		LastLines:  append([]string(nil), p.lastLines...),
		ResumeHint: p.resumeHint,
		At:         at,
	})
}

func (p *Parser) event(event Event) Event {
	event.RunID = p.runID
	if event.At.IsZero() {
		event.At = p.now().UTC()
	}
	if event.Confidence == "" {
		event.Confidence = p.confidence
	}
	event.RawLogFallback = event.RawLogFallback || p.rawLogFallback
	return event
}

func (p *Parser) currentPhase() string {
	if p.current == nil {
		return ""
	}
	return p.current.Phase
}

func (p *Parser) rememberLine(text string) {
	if p.lastLineLimit <= 0 {
		return
	}
	p.lastLines = append(p.lastLines, text)
	if len(p.lastLines) > p.lastLineLimit {
		copy(p.lastLines, p.lastLines[len(p.lastLines)-p.lastLineLimit:])
		p.lastLines = p.lastLines[:p.lastLineLimit]
	}
}
