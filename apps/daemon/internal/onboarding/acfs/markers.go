package acfs

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const DefaultPinnedRef = "main"

type MarkerLibrary struct {
	Ref string
}

func DefaultMarkerLibrary(ref string) MarkerLibrary {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = DefaultPinnedRef
	}
	return MarkerLibrary{Ref: ref}
}

type markerKind int

const (
	markerUnknown markerKind = iota
	markerStart
	markerCheckpoint
	markerEnd
	markerFail
)

type marker struct {
	kind       markerKind
	phase      string
	name       string
	key        string
	status     CheckpointStatus
	rc         int
	durationMs int64
}

var (
	startPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:\[acfs\]\s*)?phase[._ -]?start\s+([a-z0-9_.-]+)(?:\s+(.+))?\s*$`),
		regexp.MustCompile(`(?i)^\s*\[acfs\s+phase:start\]\s*([a-z0-9_.-]+)(?:\s*[:|-]\s*(.+))?\s*$`),
		regexp.MustCompile(`(?i)^\s*==>\s*(?:acfs\s+)?phase\s+([a-z0-9_.-]+)(?:\s*[:|-]\s*(.+))?\s*$`),
	}
	checkpointPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:\[acfs\]\s*)?checkpoint\s+([a-z0-9_.-]+)\s+([a-z0-9_.-]+)\s+(pass|warn|fail|skip|passed|failed|skipped)\s*$`),
		regexp.MustCompile(`(?i)^\s*\[acfs\s+checkpoint\]\s*phase=([a-z0-9_.-]+)\s+key=([a-z0-9_.-]+)\s+status=(pass|warn|fail|skip|passed|failed|skipped)\s*$`),
	}
	endPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:\[acfs\]\s*)?phase[._ -]?end\s+([a-z0-9_.-]+)(?:\s+rc=([-0-9]+))?(?:\s+durationMs=([0-9]+))?\s*$`),
		regexp.MustCompile(`(?i)^\s*\[acfs\s+phase:end\]\s*([a-z0-9_.-]+)(?:\s+rc=([-0-9]+))?(?:\s+durationMs=([0-9]+))?\s*$`),
	}
	failPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:\[acfs\]\s*)?phase[._ -]?fail\s+([a-z0-9_.-]+)(?:\s+rc=([-0-9]+))?\s*$`),
		regexp.MustCompile(`(?i)^\s*\[acfs\s+phase:fail\]\s*([a-z0-9_.-]+)(?:\s+rc=([-0-9]+))?\s*$`),
	}
)

func (l MarkerLibrary) Parse(text string) (marker, bool) {
	line := strings.TrimSpace(stripANSI(text))
	if line == "" {
		return marker{}, false
	}
	if m, ok := parseJSONMarker(line); ok {
		return m, true
	}
	for _, pattern := range startPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return marker{kind: markerStart, phase: cleanToken(match[1]), name: cleanName(group(match, 2))}, true
		}
	}
	for _, pattern := range checkpointPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return marker{
				kind:   markerCheckpoint,
				phase:  cleanToken(match[1]),
				key:    cleanToken(match[2]),
				status: normalizeCheckpoint(group(match, 3)),
			}, true
		}
	}
	for _, pattern := range endPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return marker{
				kind:       markerEnd,
				phase:      cleanToken(match[1]),
				rc:         parseInt(group(match, 2)),
				durationMs: parseInt64(group(match, 3)),
			}, true
		}
	}
	for _, pattern := range failPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return marker{kind: markerFail, phase: cleanToken(match[1]), rc: parseInt(group(match, 2))}, true
		}
	}
	return marker{}, false
}

func parseJSONMarker(line string) (marker, bool) {
	if !strings.HasPrefix(line, "{") {
		return marker{}, false
	}
	var payload struct {
		Type       string `json:"type"`
		Event      string `json:"event"`
		Phase      string `json:"phase"`
		Name       string `json:"name"`
		Key        string `json:"key"`
		Status     string `json:"status"`
		RC         *int   `json:"rc"`
		DurationMs *int64 `json:"durationMs"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return marker{}, false
	}
	eventType := firstNonEmpty(payload.Type, payload.Event)
	switch EventType(eventType) {
	case EventPhaseStart:
		return marker{kind: markerStart, phase: cleanToken(payload.Phase), name: cleanName(payload.Name)}, payload.Phase != ""
	case EventPhaseCheckpoint:
		return marker{kind: markerCheckpoint, phase: cleanToken(payload.Phase), key: cleanToken(payload.Key), status: normalizeCheckpoint(payload.Status)}, payload.Phase != "" && payload.Key != ""
	case EventPhaseEnd:
		m := marker{kind: markerEnd, phase: cleanToken(payload.Phase)}
		if payload.RC != nil {
			m.rc = *payload.RC
		}
		if payload.DurationMs != nil {
			m.durationMs = *payload.DurationMs
		}
		return m, payload.Phase != ""
	case EventPhaseFail:
		m := marker{kind: markerFail, phase: cleanToken(payload.Phase)}
		if payload.RC != nil {
			m.rc = *payload.RC
		}
		return m, payload.Phase != ""
	default:
		return marker{}, false
	}
}

func normalizeCheckpoint(status string) CheckpointStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "ok", "success":
		return CheckpointPass
	case "warn", "warning":
		return CheckpointWarn
	case "fail", "failed", "error":
		return CheckpointFail
	case "skip", "skipped":
		return CheckpointSkip
	default:
		return CheckpointWarn
	}
}

func cleanToken(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cleanName(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func group(match []string, index int) string {
	if index >= len(match) {
		return ""
	}
	return match[index]
}

func parseInt(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(s))
	return value
}

func parseInt64(s string) int64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	value, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}
