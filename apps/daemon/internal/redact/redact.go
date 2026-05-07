//go:build hoopoe_legacy_redact

// hp-mdex: this package was DISCARDED in favor of
// apps/daemon/internal/redaction/ (see commit babd8e1) but
// re-introduced as untracked files later and preserved in commit
// 2081595 pending hp-mdex resolution. Its API
// (`Redact(text) (string, []Event)`, `New()` with no Config) is
// incompatible with the canonical `redaction.Redactor`
// (`RedactText(surface, context, text) (string, []TraceEvent)`,
// `New(Config)` constructor), so a thin re-export shim is not
// viable.
//
// hp-mdex resolution: instead of deleting the files (RULE 1 forbids
// deletion without express user permission) the build tag above
// excludes both files from the default `go build` / `go test ./...`
// surface — this matches the bead's option (c) "vendor a renamed
// copy" interpreted as "preserve under a non-default build tag." The
// package no longer participates in production builds and stops
// being a parallel mutation surface beside
// apps/daemon/internal/redaction/. It is also no longer imported by
// any code in apps/daemon/ (verified via grep — only doc-comment
// references survive in audit/writer.go and audit/writer_test.go).
//
// To resurface (e.g., for archeology against the babd8e1 deletion):
//   go build -tags hoopoe_legacy_redact ./...
//   go test  -tags hoopoe_legacy_redact ./internal/redact/...
//
// If you need a redactor in production, import
// `github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction`
// instead — the canonical surface lives there.
//
// Original (now-historical) docblock:
//
// Package redact is the daemon-side redaction layer. It runs before any
// persistence or streaming on the daemon — bearer tokens, pairing tokens,
// model API keys, SSH passphrases, browser cookies, provider credentials,
// and sensitive paths are detected by pattern + replaced with safe
// placeholders BEFORE the audit log writer or the WebSocket fan-out sees
// the bytes.
//
// Per plan.md §5.4 line 4: "daemon logs and audit entries pass through a
// redaction layer before persistence and before streaming to clients."
//
// The desktop-side mirror lives at `apps/desktop/src/shared/redact/`. Both
// packages share the same pattern IDs and replacement strategies; a
// drift-detection test in `scripts/redactlint/` fails CI on divergence.
//
// Cross-references:
//   - hp-lxs — structured-logging library (the first consumer of redact).
//   - hp-g73 — audit log infrastructure (consumer).
//   - hp-1wg8 — Diagnostics audit-log explorer UI (renders redaction stats).
//   - docs/observability.md — pattern table + how to add a pattern.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Redactor scrubs secrets out of free-form text. Per plan.md §5.4 + the
// hp-je1p bead, every msg, fields.*, audit-log payload, and event payload
// passes through a Redactor before persistence/streaming.
//
// The redaction layer is auditable: Redact() returns the scrubbed text plus
// a slice of pattern-IDs that fired. Callers can emit a trace-level audit
// event recording that redaction happened (which sub-pattern matched) with
// no leaked plaintext.
type Redactor struct {
	patterns []redactionPattern
}

// Event records a single redaction firing.
type Event struct {
	Pattern       string // human-readable pattern ID, e.g., "bearer-hmac"
	Count         int    // matches in this redaction call
	BytesRedacted int    // total length of matched substrings
}

type redactionPattern struct {
	id      string
	regex   *regexp.Regexp
	replace func(string) string
}

// New builds a Redactor with the canonical Hoopoe patterns.
func New() *Redactor {
	return &Redactor{patterns: defaultPatterns()}
}

// Redact runs every pattern against text and returns the redacted result
// plus an Event slice describing what fired. Empty events means no
// secrets were detected.
func (r *Redactor) Redact(text string) (string, []Event) {
	if text == "" || r == nil || len(r.patterns) == 0 {
		return text, nil
	}
	out := text
	var events []Event
	for _, p := range r.patterns {
		matches := p.regex.FindAllString(out, -1)
		if len(matches) == 0 {
			continue
		}
		bytes := 0
		for _, m := range matches {
			bytes += len(m)
		}
		out = p.regex.ReplaceAllStringFunc(out, p.replace)
		events = append(events, Event{
			Pattern:       p.id,
			Count:         len(matches),
			BytesRedacted: bytes,
		})
	}
	return out, events
}

// RedactValue redacts text inside any value in a fields map. Strings are
// scrubbed directly; nested maps recurse; arrays are walked element-wise;
// other types are returned unchanged.
func (r *Redactor) RedactValue(value any) (any, []Event) {
	switch v := value.(type) {
	case string:
		return r.Redact(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		var allEvents []Event
		for k, child := range v {
			redacted, events := r.RedactValue(child)
			out[k] = redacted
			allEvents = appendEvents(allEvents, events)
		}
		return out, allEvents
	case []any:
		out := make([]any, len(v))
		var allEvents []Event
		for i, child := range v {
			redacted, events := r.RedactValue(child)
			out[i] = redacted
			allEvents = appendEvents(allEvents, events)
		}
		return out, allEvents
	default:
		return value, nil
	}
}

// PatternIDs returns the list of pattern IDs in the order they fire. Used
// by the drift-detection test.
func (r *Redactor) PatternIDs() []string {
	out := make([]string, 0, len(r.patterns))
	for _, p := range r.patterns {
		out = append(out, p.id)
	}
	return out
}

func appendEvents(a, b []Event) []Event {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	return append(a, b...)
}

// TraceEvent is the structured shape emitted for each redaction firing.
// Per hp-je1p:
//
//	{ts, redactor, pattern_id, context, bytes_redacted}
//
// Trace events are themselves redacted (no leaked secret content). The
// daemon's audit-log writer (hp-g73) is the typical sink; logger emits at
// trace level.
type TraceEvent struct {
	TS            time.Time `json:"ts"`
	Redactor      string    `json:"redactor"`     // 'audit' | 'events' | 'logger' | 'adapter:<name>'
	PatternID     string    `json:"pattern_id"`
	Context       string    `json:"context"`      // dotted field path, e.g., 'audit.command_preview'
	BytesRedacted int       `json:"bytes_redacted"`
}

// Stats accumulates redaction counts per pattern. Diagnostics renders the
// breakdown so operators can verify redaction is firing.
type Stats struct {
	mu     sync.Mutex
	counts map[string]int
	bytes  map[string]int
}

// NewStats constructs an empty Stats accumulator.
func NewStats() *Stats {
	return &Stats{
		counts: make(map[string]int),
		bytes:  make(map[string]int),
	}
}

// Record increments the count and byte total for each Event.
func (s *Stats) Record(events []Event) {
	if s == nil || len(events) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range events {
		s.counts[e.Pattern] += e.Count
		s.bytes[e.Pattern] += e.BytesRedacted
	}
}

// Snapshot returns a copy of the current stats. Caller may mutate the
// returned maps.
func (s *Stats) Snapshot() (counts map[string]int, bytes map[string]int) {
	if s == nil {
		return map[string]int{}, map[string]int{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := make(map[string]int, len(s.counts))
	b := make(map[string]int, len(s.bytes))
	for k, v := range s.counts {
		c[k] = v
	}
	for k, v := range s.bytes {
		b[k] = v
	}
	return c, b
}

// hashTag returns sha256:<8 hex chars> for a matched secret. Stable across
// occurrences so log readers can correlate without recovering plaintext.
func hashTag(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:4])
}

func providerKeyReplace(prefix string) func(string) string {
	return func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return prefix + "[redacted-sha256:" + hex.EncodeToString(sum[:4]) + "]"
	}
}

// emailLast4 keeps only the last 4 chars of the local part + the domain.
func emailLast4(s string) string {
	at := strings.LastIndex(s, "@")
	if at <= 0 {
		return s
	}
	local := s[:at]
	domain := s[at:]
	keep := 4
	if len(local) < keep {
		keep = len(local)
	}
	return "***" + local[len(local)-keep:] + domain
}

// defaultPatterns is the canonical Hoopoe redaction set. ORDER MATTERS:
// specific patterns (private-key blocks, sk-ant-) run before broader ones
// (sk-) so the longer prefix wins. Don't reorder without a corresponding
// test update.
//
// Pattern IDs MUST stay in sync with `apps/desktop/src/shared/redact/redact.ts`;
// the drift-detection test in `scripts/redactlint/` enforces this.
func defaultPatterns() []redactionPattern {
	return []redactionPattern{
		// Private key blocks (RSA / OpenSSH / EC / DSA / PGP).
		{
			id:      "private-key-block",
			regex:   regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+PRIVATE KEY( BLOCK)?-----.*?-----END [A-Z ]+PRIVATE KEY( BLOCK)?-----`),
			replace: func(string) string { return "[private-key-redacted]" },
		},
		// JWT-shaped bearer tokens.
		{
			id:      "bearer-hmac",
			regex:   regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`),
			replace: hashTag,
		},
		// Anthropic key prefix (`sk-ant-...`). Match before the broader `sk-`.
		{
			id:      "provider-key-anthropic",
			regex:   regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{16,}\b`),
			replace: providerKeyReplace("sk-ant-"),
		},
		// Generic `sk-...` (OpenAI-shaped).
		{
			id:      "provider-key-openai",
			regex:   regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}\b`),
			replace: providerKeyReplace("sk-"),
		},
		// Google AIza key prefix.
		{
			id:      "provider-key-google",
			regex:   regexp.MustCompile(`\bAIza[A-Za-z0-9_\-]{35}\b`),
			replace: providerKeyReplace("AIza-"),
		},
		// AWS access key id.
		{
			id:      "provider-key-aws",
			regex:   regexp.MustCompile(`\bAKIA[A-Z0-9]{16}\b`),
			replace: providerKeyReplace("AKIA-"),
		},
		// GitHub personal/access tokens.
		{
			id:      "provider-key-github",
			regex:   regexp.MustCompile(`\bghp_[A-Za-z0-9]{36,}\b`),
			replace: providerKeyReplace("ghp-"),
		},
		// Slack tokens.
		{
			id:      "provider-key-slack",
			regex:   regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}\b`),
			replace: providerKeyReplace("xox-"),
		},
		// Hoopoe 12-char Crockford pairing tokens.
		{
			id:      "pairing-token",
			regex:   regexp.MustCompile(`\bH-[ABCDEFGHJKMNPQRSTVWXYZ0-9]{11}\b`),
			replace: func(string) string { return "[pairing-token-redacted]" },
		},
		// HTTP Authorization header — case-insensitive.
		{
			id:      "http-authorization-header",
			regex:   regexp.MustCompile(`(?i)\bAuthorization\s*:\s*[^\r\n]+`),
			replace: func(string) string { return "Authorization: [redacted-header]" },
		},
		// HTTP Cookie / Set-Cookie headers.
		{
			id:      "http-cookie-header",
			regex:   regexp.MustCompile(`(?i)\bSet-Cookie\s*:\s*[^\r\n]+`),
			replace: func(string) string { return "Set-Cookie: [redacted-header]" },
		},
		{
			id:      "http-cookie-header-request",
			regex:   regexp.MustCompile(`(?i)\bCookie\s*:\s*[^\r\n]+`),
			replace: func(string) string { return "Cookie: [redacted-header]" },
		},
		// SSH passphrases.
		{
			id:      "ssh-passphrase",
			regex:   regexp.MustCompile(`(?i)\bpassphrase\s*[:=]\s*\S+`),
			replace: func(string) string { return "passphrase=[redacted]" },
		},
		// Browser session cookies for AI provider sites.
		{
			id:      "chatgpt-cookie",
			regex:   regexp.MustCompile(`(?i)\b__Secure-next-auth\.session-token\s*=\s*[A-Za-z0-9._\-]+`),
			replace: func(string) string { return "__Secure-next-auth.session-token=[redacted]" },
		},
		{
			id:      "claude-ai-cookie",
			regex:   regexp.MustCompile(`(?i)\bsessionKey\s*=\s*sk-ant-[A-Za-z0-9_\-]+`),
			replace: func(string) string { return "sessionKey=[redacted]" },
		},
		{
			id:      "openai-com-cookie",
			regex:   regexp.MustCompile(`(?i)\b__Host-next-auth\.csrf-token\s*=\s*[A-Za-z0-9._%\-]+`),
			replace: func(string) string { return "__Host-next-auth.csrf-token=[redacted]" },
		},
		// Telegram bot tokens.
		{
			id:      "telegram-bot-token",
			regex:   regexp.MustCompile(`\b\d{8,10}:[A-Za-z0-9_\-]{30,}\b`),
			replace: func(string) string { return "[telegram-bot-token-redacted]" },
		},
		// Email addresses → keep last 4 chars of local part.
		{
			id:      "email-address",
			regex:   regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
			replace: emailLast4,
		},
		// Sensitive file paths.
		{
			id:      "ssh-key-path",
			regex:   regexp.MustCompile(`(?:^|\s|"|')~?/?(?:[\w/\-]*?)?\.ssh/[\w./\-]+`),
			replace: func(string) string { return "[ssh-key-path-redacted]" },
		},
		{
			id:      "shadow-file-path",
			regex:   regexp.MustCompile(`/etc/shadow\b`),
			replace: func(string) string { return "[shadow-path-redacted]" },
		},
		{
			id:      "macos-keychain-path",
			regex:   regexp.MustCompile(`/private/var/db/[\w./\-]+`),
			replace: func(string) string { return "[keychain-path-redacted]" },
		},
		{
			id:      "oracle-profile-path",
			regex:   regexp.MustCompile(`(?:^|\s|"|')~?/?\.config/oracle/profiles/[\w./\-]+`),
			replace: func(string) string { return "[oracle-profile-path-redacted]" },
		},
	}
}
