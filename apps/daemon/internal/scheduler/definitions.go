package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type DefinitionDisk struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Kind                 JobKind           `json:"kind"`
	Version              int               `json:"version"`
	Schedule             string            `json:"schedule"`
	ProjectScope         string            `json:"project_scope,omitempty"`
	EnabledToolsets      []string          `json:"enabled_toolsets,omitempty"`
	CapabilitiesRequired []string          `json:"capabilities_required,omitempty"`
	CapabilitiesOptional []string          `json:"capabilities_optional,omitempty"`
	Script               string            `json:"script,omitempty"`
	Skills               []string          `json:"skills,omitempty"`
	Prompt               string            `json:"prompt,omitempty"`
	Deliver              string            `json:"deliver,omitempty"`
	Repeat               string            `json:"repeat,omitempty"`
	Paused               bool              `json:"paused"`
	Timeout              string            `json:"timeout,omitempty"`
	MaxConcurrency       int               `json:"max_concurrency,omitempty"`
	MisfirePolicy        MisfirePolicy     `json:"misfire_policy,omitempty"`
	RetryPolicy          RetryPolicy       `json:"retry_policy,omitempty"`
	DeadLetterAfter      int               `json:"dead_letter_after,omitempty"`
	AuditAlways          *bool             `json:"audit_always,omitempty"`
	Extra                map[string]string `json:"extra,omitempty"`
}

func LoadDefinitions(ctx context.Context, dir string) ([]Definition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defs := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}
		def, err := LoadDefinitionFile(ctx, filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func LoadDefinitionFile(ctx context.Context, path string) (Definition, error) {
	if err := ctx.Err(); err != nil {
		return Definition{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	var disk DefinitionDisk
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &disk); err != nil {
			return Definition{}, err
		}
	case ".yaml", ".yml":
		parsed, err := parseSimpleYAML(data)
		if err != nil {
			return Definition{}, err
		}
		disk = parsed
	default:
		return Definition{}, fmt.Errorf("%w: unsupported definition extension %q", ErrInvalidDefinition, filepath.Ext(path))
	}
	return disk.toDefinition()
}

func WriteDefinitionFile(ctx context.Context, path string, definition Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	disk := DefinitionDisk{
		ID:                   definition.ID,
		Name:                 definition.Name,
		Kind:                 definition.Kind,
		Version:              definition.Version,
		Schedule:             definition.Schedule.String(),
		ProjectScope:         definition.ProjectScope,
		EnabledToolsets:      definition.EnabledToolsets,
		CapabilitiesRequired: definition.CapabilitiesRequired,
		CapabilitiesOptional: definition.CapabilitiesOptional,
		Script:               definition.Script,
		Skills:               definition.Skills,
		Prompt:               definition.Prompt,
		Deliver:              definition.Deliver,
		Repeat:               repeatString(definition.Repeat),
		Paused:               definition.Paused,
		Timeout:              durationString(definition.Timeout),
		MaxConcurrency:       definition.MaxConcurrency,
		MisfirePolicy:        definition.MisfirePolicy,
		RetryPolicy:          definition.RetryPolicy,
		DeadLetterAfter:      definition.DeadLetterAfter,
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("sync scheduler definitions directory: %w", err)
	}
	defer dir.Close()
	_ = dir.Sync()
	return nil
}

func (d DefinitionDisk) toDefinition() (Definition, error) {
	timeout, err := parseOptionalDuration(d.Timeout)
	if err != nil {
		return Definition{}, err
	}
	repeat, err := parseRepeat(d.Repeat)
	if err != nil {
		return Definition{}, err
	}
	schedule, err := ParseSchedule(d.Schedule)
	if err != nil {
		return Definition{}, err
	}
	def := Definition{
		ID:                   d.ID,
		Name:                 d.Name,
		Kind:                 d.Kind,
		Version:              d.Version,
		Schedule:             schedule,
		ProjectScope:         d.ProjectScope,
		EnabledToolsets:      append([]string(nil), d.EnabledToolsets...),
		CapabilitiesRequired: append([]string(nil), d.CapabilitiesRequired...),
		CapabilitiesOptional: append([]string(nil), d.CapabilitiesOptional...),
		Script:               d.Script,
		Skills:               append([]string(nil), d.Skills...),
		Prompt:               d.Prompt,
		Deliver:              d.Deliver,
		Repeat:               repeat,
		Paused:               d.Paused,
		Timeout:              timeout,
		MaxConcurrency:       d.MaxConcurrency,
		MisfirePolicy:        d.MisfirePolicy,
		RetryPolicy:          d.RetryPolicy,
		DeadLetterAfter:      d.DeadLetterAfter,
	}
	def = def.normalized()
	if d.AuditAlways != nil {
		def.AuditAlways = *d.AuditAlways
	}
	return def, def.Validate()
}

func ParseSchedule(raw string) (Schedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "on demand" || raw == "on_demand" {
		return Schedule{Type: ScheduleOnDemand}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "every "); ok {
		duration, err := parseDuration(rest)
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{Type: ScheduleInterval, Interval: duration}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "on event:"); ok {
		event := strings.TrimSpace(rest)
		if event == "" {
			return Schedule{}, fmt.Errorf("%w: empty event schedule", ErrInvalidDefinition)
		}
		return Schedule{Type: ScheduleEvent, Event: event}, nil
	}
	if _, err := parseCron(raw); err != nil {
		return Schedule{}, err
	}
	return Schedule{Type: ScheduleCron, Cron: raw}, nil
}

func (s Schedule) String() string {
	switch s.Type {
	case ScheduleInterval:
		return "every " + durationString(s.Interval)
	case ScheduleEvent:
		return "on event: " + s.Event
	case ScheduleOnDemand:
		return "on demand"
	case ScheduleCron:
		return s.Cron
	default:
		return ""
	}
}

func parseSimpleYAML(data []byte) (DefinitionDisk, error) {
	var disk DefinitionDisk
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return DefinitionDisk{}, fmt.Errorf("%w: yaml line %d has no key/value separator", ErrInvalidDefinition, lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch key {
		case "id":
			disk.ID = value
		case "name":
			disk.Name = value
		case "kind":
			disk.Kind = JobKind(value)
		case "version":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return DefinitionDisk{}, err
			}
			disk.Version = parsed
		case "schedule":
			disk.Schedule = value
		case "script":
			disk.Script = value
		case "prompt":
			disk.Prompt = value
		case "deliver":
			disk.Deliver = value
		case "repeat":
			disk.Repeat = value
		case "paused":
			disk.Paused = value == "true"
		case "timeout":
			disk.Timeout = value
		case "max_concurrency":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return DefinitionDisk{}, err
			}
			disk.MaxConcurrency = parsed
		case "misfire_policy":
			disk.MisfirePolicy = MisfirePolicy(value)
		case "retry_policy":
			disk.RetryPolicy = RetryPolicy(value)
		case "dead_letter_after":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return DefinitionDisk{}, err
			}
			disk.DeadLetterAfter = parsed
		}
	}
	return disk, nil
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseDuration(raw)
}

func parseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%w: invalid day duration %q", ErrInvalidDefinition, raw)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%w: invalid duration %q", ErrInvalidDefinition, raw)
	}
	return duration, nil
}

func parseRepeat(raw string) (Repeat, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "forever" {
		return RepeatForever(), nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return Repeat{}, fmt.Errorf("%w: invalid repeat %q", ErrInvalidDefinition, raw)
	}
	return RepeatCount(n), nil
}

func repeatString(repeat Repeat) string {
	if repeat.Forever || repeat.Limit == 0 {
		return "forever"
	}
	return strconv.Itoa(repeat.Limit)
}

func durationString(duration time.Duration) string {
	if duration == 0 {
		return ""
	}
	if duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(duration/(24*time.Hour)))
	}
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}
