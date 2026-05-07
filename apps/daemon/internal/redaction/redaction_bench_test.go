// hp-uws — focused Go benchmarks for the redaction hot paths.
//
// Profiling audit (the bead's evidence) ran
// `rch exec -- go test -bench=. -benchmem ./internal/redaction/`
// and got zero Benchmark rows because no benchmark functions
// existed in the package. Without ns/op + B/op + allocs/op coverage,
// allocation hot spots in:
//
//   - RedactText (per-pattern regex match → ReplaceAllStringFunc)
//   - RedactValue nested JSON walks (map[string]any / []any /
//     reflect-based typed structs)
//   - base64-wrapped secret unwrapping
//   - Stats accumulation
//
// can't be detected. These benchmarks lock down the surfaces that
// matter operationally:
//
//   - canonical secrets (per pattern class)
//   - large benign payloads (no matches — the regex engine still
//     walks them)
//   - multi-secret payloads (many matches in one pass)
//   - nested maps/arrays (the recursive RedactValue walk)
//   - typed-struct payload (reflect path, the hp-cy4 production wiring)
//   - concurrent stats snapshots (the SnapshotStats lock contention)
//
// Run via: `go test -bench=. -benchmem -run=^$ ./internal/redaction/`
// or `rch exec -- go test -bench=. -benchmem ./internal/redaction/`.

package redaction

import (
	"strings"
	"testing"
)

// BenchmarkRedactTextNoMatches measures the cost of a redactor pass
// over a large benign payload — every pattern's regex still walks
// the bytes, so the cost is dominated by regex engine overhead, not
// allocation. Pinning ns/op + allocs/op here catches a regression
// that adds an unbounded copy or replace-with-no-match round-trip.
func BenchmarkRedactTextNoMatches(b *testing.B) {
	r := NewDefault()
	// 4 KiB of benign text, no secrets — exercises every regex with
	// zero matches.
	payload := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 92)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactText(SurfaceLogger, "bench", payload)
	}
}

// BenchmarkRedactTextProviderKey measures the canonical hot path:
// a payload containing a `sk-…` provider key. Every Publish.Data
// passing through redactStreamedEvent hits this exact shape when a
// commit message or audit entry leaks a key. The replace produces
// one [provider-key-…-redacted] substitution.
func BenchmarkRedactTextProviderKey(b *testing.B) {
	r := NewDefault()
	payload := "log line: api call failed with KEY=sk-abcdef0123456789ABCDEF0123456789 and Authorization: Bearer xyz"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactText(SurfaceLogger, "bench", payload)
	}
}

// BenchmarkRedactTextMultipleSecrets measures a payload with several
// distinct pattern classes: provider key, JWT, AWS access key,
// GitHub PAT, private-key block. Each match contributes a separate
// trace + replace; the benchmark pins both per-pattern walking cost
// and the trace-slice append cost.
func BenchmarkRedactTextMultipleSecrets(b *testing.B) {
	r := NewDefault()
	payload := strings.Join([]string{
		"sk-abcdef0123456789ABCDEF0123456789",
		"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		"AKIAIOSFODNN7EXAMPLE",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"-----BEGIN RSA PRIVATE KEY-----\nbody\n-----END RSA PRIVATE KEY-----",
	}, "\n")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactText(SurfaceLogger, "bench", payload)
	}
}

// BenchmarkRedactValueNestedMap measures the recursive RedactValue
// walk over a nested map structure — the shape that EventHub
// Publishers produce for activity payloads, mail bodies, and
// adapter output snapshots. Hits the map[string]any → recursive
// RedactValue → joinContext key path that allocates a new context
// string per level.
func BenchmarkRedactValueNestedMap(b *testing.B) {
	r := NewDefault()
	payload := map[string]any{
		"tool": "git",
		"actor": map[string]any{
			"id":   "agent-1",
			"key":  "sk-abcdef0123456789ABCDEF0123456789",
			"role": "owner",
		},
		"events": []any{
			map[string]any{"type": "commit", "msg": "update auth flow"},
			map[string]any{"type": "push", "msg": "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.x"},
			map[string]any{"type": "merge", "msg": "ok"},
		},
		"counters": map[string]any{
			"ok":     128,
			"failed": 3,
		},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactValue(SurfaceEvents, "event", payload)
	}
}

// BenchmarkRedactValueTypedStruct exercises the reflect-based
// `redactReflected` path used by hp-cy4 when EventHub publishers
// emit typed Go structs (e.g., gitevents.CommitCreatedPayload,
// activity.ActivityData). The reflect walk allocates per field
// and re-decodes via JSON round-trip — the most expensive surface
// in the redactor today.
func BenchmarkRedactValueTypedStruct(b *testing.B) {
	r := NewDefault()
	type Author struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Token string `json:"token"`
	}
	type CommitPayload struct {
		SHA     string   `json:"sha"`
		Message string   `json:"message"`
		Author  Author   `json:"author"`
		Tags    []string `json:"tags"`
	}
	payload := CommitPayload{
		SHA:     "abc123def456",
		Message: "fix auth: rotate sk-abcdef0123456789ABCDEF0123456789",
		Author: Author{
			Name:  "Bench User",
			Email: "bench@example.invalid",
			Token: "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		},
		Tags: []string{"auth", "rotation", "fix"},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactStreamedEvent(payload)
	}
}

// BenchmarkRedactTextBase64Wrapped exercises the base64-unwrap
// path where a secret hides inside a base64-encoded blob (the
// hp-… defense pattern). Decoder + inner regex pass + re-encode
// is the hot loop.
func BenchmarkRedactTextBase64Wrapped(b *testing.B) {
	r := NewDefault()
	// A base64-shaped string with a wrapped JWT inside, plus
	// surrounding payload to ensure the unwrap branch is one of
	// many paths the bench hits per iteration.
	payload := "kv: " +
		"ZXlKaGJHY2lPaUpJVXpJMU5pSjkuZXlKemRXSWlPaUl4TWpNME5UWTNPRGt3SW4wLlNmbEtnd1JKU01lS0tGMlFUNGZ3cE1lSmYzNlBPazZ5SlZfYWRRc3N3NWM=" +
		" trailing"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = r.RedactText(SurfaceLogger, "bench", payload)
	}
}

// BenchmarkSnapshotStatsConcurrent measures Stats accumulation +
// snapshot under contention. Each goroutine drives a redact pass
// then SnapshotStats; the bench pins that the per-pattern stats
// map's r.mu lock doesn't degrade catastrophically as redact
// concurrency rises (the daemon publishes from many goroutines).
func BenchmarkSnapshotStatsConcurrent(b *testing.B) {
	r := NewDefault()
	payload := "log: KEY=sk-abcdef0123456789ABCDEF0123456789"
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = r.RedactText(SurfaceLogger, "bench", payload)
			_ = r.SnapshotStats()
		}
	})
}
