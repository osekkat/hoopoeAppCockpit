package ntm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// parsers.go owns the NTM CLI/JSON output parsers + state-mapping
// helpers + Session-method receivers that derive values from parsed
// state.
//
// hp-h5yq third cut: split out of ntm.go to continue the size-outlier
// reduction. Behavior unchanged — same package, same exported
// signatures, same constants. Each parser is the same pure func it
// was before (no I/O, no Adapter receiver), and the Session methods
// remain receivers on Session in the same package.

func ParseSessionsResponse(data []byte) (SessionsResponse, error) {
	var raw struct {
		Sessions json.RawMessage `json:"sessions"`
		Count    int             `json:"count"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return SessionsResponse{}, fmt.Errorf("ntm: decode sessions list: %w", err)
	}
	resp := SessionsResponse{Count: raw.Count}
	if len(raw.Sessions) == 0 || bytes.Equal(bytes.TrimSpace(raw.Sessions), []byte("null")) {
		return resp, nil
	}
	if err := json.Unmarshal(raw.Sessions, &resp.Sessions); err != nil {
		return SessionsResponse{}, fmt.Errorf("ntm: decode sessions: %w", err)
	}
	if resp.Count == 0 {
		resp.Count = len(resp.Sessions)
	}
	return resp, nil
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("ntm: decode snapshot: %w", err)
	}
	var sessionsRaw struct {
		Sessions json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(data, &sessionsRaw); err != nil {
		return Snapshot{}, fmt.Errorf("ntm: inspect sessions: %w", err)
	}
	if len(sessionsRaw.Sessions) == 0 || bytes.Equal(bytes.TrimSpace(sessionsRaw.Sessions), []byte("null")) {
		snap.Sessions = nil
	}
	for i := range snap.Sessions {
		for j := range snap.Sessions[i].Panes {
			pane := &snap.Sessions[i].Panes[j]
			pane.UnifiedState = MapRobotState(pane.State, pane.WedgeClassification)
		}
	}
	snap.Raw = append(json.RawMessage(nil), data...)
	return snap, nil
}

func ParseTailResponse(data []byte) (TailResponse, error) {
	var tail TailResponse
	if err := json.Unmarshal(data, &tail); err != nil {
		return TailResponse{}, fmt.Errorf("ntm: decode tail: %w", err)
	}
	tail.Raw = append(json.RawMessage(nil), data...)
	return tail, nil
}

// MapRobotState normalizes raw NTM pane states into Hoopoe's unified
// state vocabulary. The wedge classification trumps any state value
// when set.
func MapRobotState(state, wedge string) string {
	if strings.EqualFold(strings.TrimSpace(wedge), "wedged") {
		return "wedged"
	}
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "IDLE":
		return "idle"
	case "TYPING":
		return "typing"
	case "THINKING":
		return "thinking"
	case "TOOL_USE", "TOOL-USE", "TOOLUSE":
		return "tool_use"
	case "COMPLETE", "COMPLETED", "DONE":
		return "complete"
	case "ERROR", "FAILED":
		return "error"
	case "ACTIVE", "RUNNING", "WORKING":
		return "working"
	case "":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}

// SessionID returns the session's stable identifier, preferring Name
// when present (operator-friendly) and falling back to the daemon-
// assigned ID.
func (s Session) SessionID() string {
	if strings.TrimSpace(s.Name) != "" {
		return strings.TrimSpace(s.Name)
	}
	return strings.TrimSpace(s.ID)
}

// NormalizedPanes synthesizes a unified pane list from both the
// session's explicit Panes slice and any agents whose pane wasn't
// already enumerated. Each pane gets its UnifiedState set via
// MapRobotState so downstream consumers don't have to reapply the
// rules.
func (s Session) NormalizedPanes() []Pane {
	out := append([]Pane(nil), s.Panes...)
	for _, agent := range s.Agents {
		pane := Pane{
			ID:             agent.Pane,
			Program:        agent.Type,
			State:          agent.State,
			LastActivityTS: agent.LastOutputTS,
			Bead:           agent.CurrentBead,
		}
		pane.UnifiedState = MapRobotState(pane.State, "")
		out = append(out, pane)
	}
	return out
}

// ParseVersion picks the first SemVer-shaped token out of an `ntm
// version` text dump. Falls back to the raw trimmed string when no
// recognizable version can be extracted, so capability probes can
// still record something rather than emit empty Version fields.
func ParseVersion(text string) string {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for _, field := range fields {
			field = strings.TrimPrefix(field, "v")
			if looksLikeVersion(field) {
				return field
			}
		}
	}
	return strings.TrimSpace(text)
}

func looksLikeVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts[:2] {
		if _, err := strconv.Atoi(strings.TrimLeft(part, "v")); err != nil {
			return false
		}
	}
	return true
}
