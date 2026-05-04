// Package redaction owns the daemon-side secret scrubbing layer used before
// audit persistence, structured log emission, adapter capture, and event
// streaming.
package redaction

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 1

type Surface string

const (
	SurfaceAudit  Surface = "audit"
	SurfaceEvents Surface = "events"
	SurfaceLogger Surface = "logger"
)

type TraceEvent struct {
	Time          time.Time `json:"ts"`
	Redactor      string    `json:"redactor"`
	PatternID     string    `json:"pattern_id"`
	Context       string    `json:"context"`
	BytesRedacted int       `json:"bytes_redacted"`
	Count         int       `json:"count"`
}

type PatternStat struct {
	PatternID     string `json:"pattern_id"`
	Count         int    `json:"count"`
	BytesRedacted int    `json:"bytes_redacted"`
}

type Stats struct {
	SchemaVersion int           `json:"schemaVersion"`
	Patterns      []PatternStat `json:"patterns"`
}

type Config struct {
	Now func() time.Time
}

type Redactor struct {
	mu       sync.Mutex
	now      func() time.Time
	patterns []pattern
	stats    map[string]PatternStat
}

type pattern struct {
	id      string
	regex   *regexp.Regexp
	replace func(string) string
}

func New(cfg Config) *Redactor {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Redactor{
		now:      now,
		patterns: defaultPatterns(),
		stats:    make(map[string]PatternStat),
	}
}

func NewDefault() *Redactor {
	return New(Config{})
}

func (r *Redactor) RedactText(surface Surface, context string, text string) (string, []TraceEvent) {
	if r == nil || text == "" {
		return text, nil
	}
	out := text
	var traces []TraceEvent
	for _, p := range r.patterns {
		matches := p.regex.FindAllString(out, -1)
		if len(matches) == 0 {
			continue
		}
		bytesRedacted := matchedBytes(matches)
		out = p.regex.ReplaceAllStringFunc(out, p.replace)
		traces = append(traces, r.trace(surface, p.id, context, bytesRedacted, len(matches)))
	}
	base64Redacted, base64Traces := r.redactBase64Wrapped(surface, context, out)
	traces = append(traces, base64Traces...)
	return base64Redacted, traces
}

func (r *Redactor) RedactValue(surface Surface, context string, value any) (any, []TraceEvent) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		return r.RedactText(surface, context, v)
	case []byte:
		redacted, traces := r.RedactText(surface, context, string(v))
		return []byte(redacted), traces
	case map[string]any:
		out := make(map[string]any, len(v))
		var traces []TraceEvent
		for key, child := range v {
			redacted, childTraces := r.RedactValue(surface, joinContext(context, key), child)
			out[key] = redacted
			traces = append(traces, childTraces...)
		}
		return out, traces
	case map[string]string:
		out := make(map[string]string, len(v))
		var traces []TraceEvent
		for key, child := range v {
			redacted, childTraces := r.RedactText(surface, joinContext(context, key), child)
			out[key] = redacted
			traces = append(traces, childTraces...)
		}
		return out, traces
	case []any:
		out := make([]any, len(v))
		var traces []TraceEvent
		for i, child := range v {
			redacted, childTraces := r.RedactValue(surface, indexedContext(context, i), child)
			out[i] = redacted
			traces = append(traces, childTraces...)
		}
		return out, traces
	case []string:
		out := make([]string, len(v))
		var traces []TraceEvent
		for i, child := range v {
			redacted, childTraces := r.RedactText(surface, indexedContext(context, i), child)
			out[i] = redacted
			traces = append(traces, childTraces...)
		}
		return out, traces
	default:
		return r.redactReflected(surface, context, value)
	}
}

// redactReflected handles typed structs, pointers, named slices/maps, and
// other Go types that don't match the fast-path switch in RedactValue.
// EventHub publishers (git watcher CommitCreatedPayload, activity ActivityData,
// future producers) frequently emit typed structs as event Data; without this
// path their string fields would bypass redaction entirely.
//
// Output shape mirrors what encoding/json would produce: structs become
// map[string]any keyed by their json tag (or field name), respecting
// omitempty and json:"-". time.Time is left intact so its MarshalJSON keeps
// producing RFC3339Nano on the wire.
func (r *Redactor) redactReflected(surface Surface, context string, value any) (any, []TraceEvent) {
	if value == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(value)
	return r.redactReflectValue(surface, context, rv)
}

func (r *Redactor) redactReflectValue(surface Surface, context string, rv reflect.Value) (any, []TraceEvent) {
	if !rv.IsValid() {
		return nil, nil
	}
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, nil
		}
		return r.redactReflectValue(surface, context, rv.Elem())
	case reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
		return r.redactReflectValue(surface, context, rv.Elem())
	case reflect.String:
		text, traces := r.RedactText(surface, context, rv.String())
		return text, traces
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, nil
		}
		out := make([]any, rv.Len())
		var traces []TraceEvent
		for i := 0; i < rv.Len(); i++ {
			child, childTraces := r.redactReflectValue(surface, indexedContext(context, i), rv.Index(i))
			out[i] = child
			traces = append(traces, childTraces...)
		}
		return out, traces
	case reflect.Map:
		if rv.IsNil() {
			return nil, nil
		}
		out := make(map[string]any, rv.Len())
		var traces []TraceEvent
		iter := rv.MapRange()
		for iter.Next() {
			keyStr := fmt.Sprint(iter.Key().Interface())
			child, childTraces := r.redactReflectValue(surface, joinContext(context, keyStr), iter.Value())
			out[keyStr] = child
			traces = append(traces, childTraces...)
		}
		return out, traces
	case reflect.Struct:
		// time.Time has unexported fields and a custom JSON marshaler;
		// leaving the value intact preserves the wire format produced by
		// json.Marshal downstream.
		if rv.Type() == reflect.TypeOf(time.Time{}) {
			return rv.Interface(), nil
		}
		return r.redactReflectStruct(surface, context, rv)
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return rv.Interface(), nil
	default:
		// Channels, functions, unsafe pointers — never expected on event
		// payloads. Pass through; json.Marshal will reject them at the
		// transport layer.
		if rv.CanInterface() {
			return rv.Interface(), nil
		}
		return nil, nil
	}
}

func (r *Redactor) redactReflectStruct(surface Surface, context string, rv reflect.Value) (any, []TraceEvent) {
	t := rv.Type()
	out := make(map[string]any, rv.NumField())
	var traces []TraceEvent
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, omitEmpty, skip := jsonFieldDirective(field)
		if skip {
			continue
		}
		fv := rv.Field(i)
		if omitEmpty && fv.IsZero() {
			continue
		}
		child, childTraces := r.redactReflectValue(surface, joinContext(context, name), fv)
		out[name] = child
		traces = append(traces, childTraces...)
	}
	return out, traces
}

// jsonFieldDirective extracts the on-the-wire field name and tag flags from a
// struct field, mirroring encoding/json's parsing rules.
func jsonFieldDirective(field reflect.StructField) (name string, omitEmpty bool, skip bool) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name, false, false
	}
	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		// `json:"-"` skips the field, but `json:"-,"` keeps it under the
		// literal name "-". encoding/json behaviour preserved.
		if len(parts) == 1 {
			return "", false, true
		}
		parts[0] = "-"
	}
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}

func (r *Redactor) RedactAdapterOutput(adapter string, value any) (any, []TraceEvent) {
	surface := Surface("adapter:" + adapter)
	return r.RedactValue(surface, "adapter.output", value)
}

func (r *Redactor) RedactStreamedEvent(value any) (any, []TraceEvent) {
	return r.RedactValue(SurfaceEvents, "event", value)
}

func (r *Redactor) SnapshotStats() Stats {
	if r == nil {
		return Stats{SchemaVersion: SchemaVersion}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Stats{
		SchemaVersion: SchemaVersion,
		Patterns:      make([]PatternStat, 0, len(r.stats)),
	}
	for _, stat := range r.stats {
		out.Patterns = append(out.Patterns, stat)
	}
	sort.Slice(out.Patterns, func(i, j int) bool {
		return out.Patterns[i].PatternID < out.Patterns[j].PatternID
	})
	return out
}

func (r *Redactor) trace(surface Surface, patternID string, context string, bytesRedacted int, count int) TraceEvent {
	ev := TraceEvent{
		Time:          r.now().UTC(),
		Redactor:      string(surface),
		PatternID:     patternID,
		Context:       context,
		BytesRedacted: bytesRedacted,
		Count:         count,
	}
	r.mu.Lock()
	stat := r.stats[patternID]
	stat.PatternID = patternID
	stat.Count += count
	stat.BytesRedacted += bytesRedacted
	r.stats[patternID] = stat
	r.mu.Unlock()
	return ev
}

func matchedBytes(matches []string) int {
	total := 0
	for _, match := range matches {
		total += len(match)
	}
	return total
}

func joinContext(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func indexedContext(parent string, index int) string {
	return fmt.Sprintf("%s[%d]", parent, index)
}

func (r *Redactor) redactBase64Wrapped(surface Surface, context string, text string) (string, []TraceEvent) {
	candidates := base64Like.FindAllString(text, -1)
	if len(candidates) == 0 {
		return text, nil
	}
	out := text
	var traces []TraceEvent
	for _, candidate := range candidates {
		decoded, ok := decodeCandidate(candidate)
		if !ok {
			continue
		}
		redacted, innerTraces := r.RedactText(surface, context+".base64", decoded)
		if len(innerTraces) == 0 || redacted == decoded {
			continue
		}
		out = strings.ReplaceAll(out, candidate, "[base64-secret-redacted]")
		traces = append(traces, r.trace(surface, "base64-wrapped-secret", context, len(candidate), 1))
	}
	return out, traces
}

func decodeCandidate(candidate string) (string, bool) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(candidate)
		if err != nil {
			continue
		}
		if utf8.Valid(decoded) {
			return string(decoded), true
		}
	}
	return "", false
}

var base64Like = regexp.MustCompile(`\b[A-Za-z0-9+/_-]{32,}={0,2}\b`)
