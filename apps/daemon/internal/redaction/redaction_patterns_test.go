package redaction

import (
	"strings"
	"sync"
	"testing"
)

// Per-pattern coverage tests, ported from a parallel internal/redact/
// implementation that landed untracked alongside this canonical package.
// Adapted to the canonical Surface/context API and pattern-ID names
// (provider-key-sk, http-header-*, browser-cookie-*, macos-private-db-path).

func TestRedactPrivateKeyBlock(t *testing.T) {
	r := NewDefault()
	// Canonical regex covers RSA / DSA / OPENSSH / EC / PGP and bare PRIVATE KEY.
	for _, kind := range []string{"RSA", "DSA", "OPENSSH", "EC", "PGP"} {
		input := "loading: -----BEGIN " + kind + " PRIVATE KEY-----\nbody\n-----END " + kind + " PRIVATE KEY-----\nok"
		if kind == "PGP" {
			input = "loading: -----BEGIN " + kind + " PRIVATE KEY BLOCK-----\nbody\n-----END " + kind + " PRIVATE KEY BLOCK-----\nok"
		}
		out, traces := r.RedactText(SurfaceAudit, "test", input)
		if strings.Contains(out, "BEGIN") {
			t.Errorf("%s key block not redacted: %s", kind, out)
		}
		if !strings.Contains(out, "[private-key-redacted]") {
			t.Errorf("%s expected placeholder", kind)
		}
		hit := false
		for _, ev := range traces {
			if ev.PatternID == "private-key-block" {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%s expected private-key-block trace, got %+v", kind, traces)
		}
	}
}

func TestRedactBearerHMAC(t *testing.T) {
	r := NewDefault()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	out, traces := r.RedactText(SurfaceAudit, "test", jwt)
	if strings.Contains(out, jwt) {
		t.Errorf("JWT not redacted")
	}
	if !strings.Contains(out, "sha256:") {
		t.Errorf("expected sha256 tag, got %s", out)
	}
	if len(traces) == 0 || traces[0].PatternID != "bearer-hmac" {
		t.Errorf("expected bearer-hmac trace, got %+v", traces)
	}
}

func TestRedactProviderKeys(t *testing.T) {
	cases := []struct {
		name, input, pattern string
	}{
		{"openai", "key=sk-abcdef0123456789ABCDEF0123456789", "provider-key-sk"},
		{"anthropic", "key=sk-ant-api03-abcdefABCDEF1234567890", "provider-key-anthropic"},
		{"google", "key=AIzaSyA0123456789abcdefghijklmnopqrstuv", "provider-key-google"},
		{"aws", "key=AKIAIOSFODNN7EXAMPLE", "provider-key-aws"},
		{"github", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "provider-key-github"},
		{"slack", "xoxb-123456789012-1234567890123-AbCdEfGhIjKl", "provider-key-slack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewDefault()
			out, traces := r.RedactText(SurfaceAudit, "test", tc.input)
			if out == tc.input {
				t.Errorf("not redacted: %s", out)
			}
			found := false
			for _, ev := range traces {
				if ev.PatternID == tc.pattern {
					found = true
				}
			}
			if !found {
				t.Errorf("missing pattern %s in %+v", tc.pattern, traces)
			}
		})
	}
}

func TestRedactPairingToken(t *testing.T) {
	r := NewDefault()
	out, traces := r.RedactText(SurfaceAudit, "test", "paired with H-ABCDEFGHJKM in 5 sec")
	if !strings.Contains(out, "[pairing-token-redacted]") {
		t.Errorf("not redacted: %s", out)
	}
	if len(traces) == 0 || traces[0].PatternID != "pairing-token" {
		t.Errorf("expected pairing-token trace, got %+v", traces)
	}
}

func TestRedactHTTPAuthorizationHeader(t *testing.T) {
	r := NewDefault()
	out, traces := r.RedactText(SurfaceAudit, "test", "Authorization: Bearer sk-abcdef0123456789ABCDEF0123456789")
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("auth header leaked: %s", out)
	}
	hit := false
	for _, ev := range traces {
		if ev.PatternID == "http-header-authorization" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected http-header-authorization trace, got %+v", traces)
	}
}

func TestRedactHTTPCookieHeaders(t *testing.T) {
	r := NewDefault()
	out, _ := r.RedactText(SurfaceAudit, "test", "Set-Cookie: session=abcdefghijklmnop; Path=/")
	if strings.Contains(out, "abcdefghijklmnop") {
		t.Errorf("set-cookie header leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted-header]") {
		t.Errorf("expected placeholder: %s", out)
	}
	out2, _ := r.RedactText(SurfaceAudit, "test", "Cookie: session=abcdefghijklmnop")
	if strings.Contains(out2, "abcdefghijklmnop") {
		t.Errorf("cookie header leaked: %s", out2)
	}
}

func TestRedactSSHPassphrase(t *testing.T) {
	r := NewDefault()
	out, traces := r.RedactText(SurfaceAudit, "test", "Passphrase=hunter2")
	if strings.Contains(out, "hunter2") {
		t.Errorf("passphrase leaked")
	}
	if len(traces) == 0 || traces[0].PatternID != "ssh-passphrase" {
		t.Errorf("expected ssh-passphrase trace, got %+v", traces)
	}
}

func TestRedactBrowserCookieChatGPT(t *testing.T) {
	r := NewDefault()
	out, traces := r.RedactText(SurfaceAudit, "test", "__Secure-next-auth.session-token=abc.def.ghi")
	if strings.Contains(out, "abc.def.ghi") {
		t.Errorf("not redacted: %s", out)
	}
	if len(traces) == 0 {
		t.Error("no traces fired")
	}
}

func TestRedactBrowserCookieNextAuthCsrf(t *testing.T) {
	r := NewDefault()
	// auth.js / next-auth CSRF cookie. URL-encoded `+` (%2B) appears in
	// real values — the character class must accept `%`.
	out, traces := r.RedactText(SurfaceAudit, "test", "__Host-next-auth.csrf-token=abc%2Bdef.ghi")
	if strings.Contains(out, "abc%2Bdef.ghi") {
		t.Errorf("csrf cookie leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("expected placeholder: %s", out)
	}
	hit := false
	for _, ev := range traces {
		if ev.PatternID == "browser-cookie-next-auth-csrf" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected browser-cookie-next-auth-csrf trace, got %+v", traces)
	}
}

func TestRedactBrowserCookieClaudeSessionKey(t *testing.T) {
	r := NewDefault()
	// Claude.ai session cookie. Suffix is too short for provider-key-anthropic
	// (needs 16+ chars) and too unstructured for browser-cookie-claude (which
	// requires "claude" or "anthropic" in the cookie name itself).
	out, traces := r.RedactText(SurfaceAudit, "test", "sessionKey=sk-ant-api03-abc")
	if strings.Contains(out, "api03-abc") {
		t.Errorf("sessionKey leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("expected placeholder: %s", out)
	}
	hit := false
	for _, ev := range traces {
		if ev.PatternID == "browser-cookie-claude-sessionkey" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected browser-cookie-claude-sessionkey trace, got %+v", traces)
	}
}

func TestRedactTelegramBotToken(t *testing.T) {
	r := NewDefault()
	out, _ := r.RedactText(SurfaceAudit, "test", "token=123456789:AAEhBP0avQ7AdEXAMPLE_THISisJUST_a_PLACEHOLDER")
	if strings.Contains(out, "123456789:AAE") {
		t.Errorf("telegram token leaked: %s", out)
	}
}

func TestRedactEmailLast4(t *testing.T) {
	r := NewDefault()
	out, _ := r.RedactText(SurfaceAudit, "test", "notify alice@example.com please")
	if strings.Contains(out, "alice@example.com") {
		t.Errorf("email leaked")
	}
	if !strings.Contains(out, "@example.com") {
		t.Errorf("expected last-4 + domain")
	}
}

func TestRedactSensitiveFilePaths(t *testing.T) {
	r := NewDefault()
	for _, in := range []string{
		"reading ~/.ssh/id_rsa",
		"shadow at /etc/shadow",
		"keychain at /private/var/db/login.keychain-db",
		"loaded ~/.config/oracle/profiles/main",
	} {
		out, traces := r.RedactText(SurfaceAudit, "test", in)
		if out == in {
			t.Errorf("not redacted: %s", in)
		}
		if len(traces) == 0 {
			t.Errorf("no traces: %s", in)
		}
	}
}

func TestRedactNestedJSON(t *testing.T) {
	r := NewDefault()
	out, _ := r.RedactText(SurfaceAudit, "test", `{"args":{"key":"sk-abcdef0123456789ABCDEF0123456789"}}`)
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("not redacted: %s", out)
	}
}

func TestRedactURLQuery(t *testing.T) {
	r := NewDefault()
	out, _ := r.RedactText(SurfaceAudit, "test", "GET /v1/jobs?token=sk-abcdef0123456789ABCDEF0123456789&action=run")
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("not redacted: %s", out)
	}
}

func TestRedactMultiLineValue(t *testing.T) {
	r := NewDefault()
	input := "payload <<EOF\nline1\nsk-abcdef0123456789ABCDEF0123456789\nline3\nEOF"
	out, _ := r.RedactText(SurfaceAudit, "test", input)
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("multi-line secret leaked: %s", out)
	}
}

// Base64-wrapped secrets are redacted by the canonical implementation's
// redactBase64Wrapped pass — distinct from the parallel `redact/` package,
// which deliberately let them pass through.
func TestRedactBase64WrappedDirect(t *testing.T) {
	r := NewDefault()
	encoded := "c2stYWJjZGVmMDEyMzQ1Njc4OUFCQ0RFRjAxMjM0NTY3ODk="
	out, traces := r.RedactText(SurfaceAudit, "test", "blob="+encoded)
	if strings.Contains(out, encoded) {
		t.Errorf("base64-wrapped secret should be redacted: %s", out)
	}
	hit := false
	for _, ev := range traces {
		if ev.PatternID == "base64-wrapped-secret" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected base64-wrapped-secret trace, got %+v", traces)
	}
}

func TestRedactMultipleSecretsOneInput(t *testing.T) {
	r := NewDefault()
	input := "k1=sk-abcdef0123456789ABCDEF0123456789 k2=AKIAIOSFODNN7EXAMPLE k3=ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	out, traces := r.RedactText(SurfaceAudit, "test", input)
	for _, leak := range []string{"sk-abcdef", "AKIAIOSF", "ghp_abc"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s leaked", leak)
		}
	}
	if len(traces) < 3 {
		t.Errorf("expected ≥3 traces, got %d", len(traces))
	}
}

func TestRedactValueRecursesNestedMapsAndArrays(t *testing.T) {
	r := NewDefault()
	input := map[string]any{
		"outer": map[string]any{
			"key": "sk-abcdef0123456789ABCDEF0123456789",
			"arr": []any{"AKIAIOSFODNN7EXAMPLE", "no secret"},
		},
	}
	out, traces := r.RedactValue(SurfaceAudit, "test", input)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("not a map: %T", out)
	}
	inner, ok := outMap["outer"].(map[string]any)
	if !ok {
		t.Fatalf("not nested: %T", outMap["outer"])
	}
	if v, _ := inner["key"].(string); strings.Contains(v, "sk-abcdef") {
		t.Errorf("nested key leaked: %s", v)
	}
	arr, _ := inner["arr"].([]any)
	if v, _ := arr[0].(string); strings.Contains(v, "AKIAIOSF") {
		t.Errorf("nested array leaked: %s", v)
	}
	if len(traces) == 0 {
		t.Errorf("no traces from nested input")
	}
}

func TestRedactBytesRedactedReportsExactMatchLength(t *testing.T) {
	r := NewDefault()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	_, traces := r.RedactText(SurfaceAudit, "test", jwt)
	if len(traces) == 0 {
		t.Fatal("expected at least one trace")
	}
	if traces[0].BytesRedacted != len(jwt) {
		t.Errorf("expected bytesRedacted=%d, got %d", len(jwt), traces[0].BytesRedacted)
	}
}

func TestRedactEmptyInputProducesNoTraces(t *testing.T) {
	r := NewDefault()
	out, traces := r.RedactText(SurfaceAudit, "test", "")
	if out != "" {
		t.Errorf("got %q", out)
	}
	if len(traces) != 0 {
		t.Errorf("got traces: %v", traces)
	}
}

// Concurrent redactions exercise the Redactor's stats-map mutex.
func TestRedactStatsAccumulateUnderConcurrency(t *testing.T) {
	r := NewDefault()
	const goroutines = 8
	const perGoroutine = 50
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				_, _ = r.RedactText(SurfaceAudit, "test", jwt)
			}
		}()
	}
	wg.Wait()
	stats := r.SnapshotStats()
	for _, stat := range stats.Patterns {
		if stat.PatternID == "bearer-hmac" {
			if stat.Count != goroutines*perGoroutine {
				t.Errorf("bearer-hmac count = %d, want %d", stat.Count, goroutines*perGoroutine)
			}
			return
		}
	}
	t.Errorf("bearer-hmac pattern not in stats: %#v", stats.Patterns)
}

// SnapshotStats on a nil Redactor must not panic.
func TestRedactSnapshotStatsNilSafe(t *testing.T) {
	var r *Redactor
	stats := r.SnapshotStats()
	if stats.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", stats.SchemaVersion, SchemaVersion)
	}
	if len(stats.Patterns) != 0 {
		t.Errorf("expected no patterns, got %#v", stats.Patterns)
	}
}

// RedactText on a nil Redactor must not panic and must pass text through.
func TestRedactTextNilSafe(t *testing.T) {
	var r *Redactor
	out, traces := r.RedactText(SurfaceAudit, "test", "Authorization: Bearer abc")
	if out != "Authorization: Bearer abc" {
		t.Errorf("nil redactor mutated text: %q", out)
	}
	if traces != nil {
		t.Errorf("nil redactor returned traces: %+v", traces)
	}
}

// SnapshotStats must emit Patterns sorted by PatternID so Go and TS produce
// the same key order — the TS mirror in apps/desktop/src/shared/redact/
// already sorts; without sorting on the Go side, diagnostics consumers see
// non-deterministic orderings depending on map iteration.
func TestRedactSnapshotStatsSortedByPatternID(t *testing.T) {
	r := NewDefault()
	// Hit several distinct patterns so the stats map has multiple entries.
	_, _ = r.RedactText(SurfaceAudit, "test", "sk-abcdef0123456789ABCDEF0123456789")
	_, _ = r.RedactText(SurfaceAudit, "test", "AKIAIOSFODNN7EXAMPLE")
	_, _ = r.RedactText(SurfaceAudit, "test", "ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	_, _ = r.RedactText(SurfaceAudit, "test", "Authorization: Bearer foo")
	_, _ = r.RedactText(SurfaceAudit, "test", "notify alice@example.com")

	stats := r.SnapshotStats()
	if len(stats.Patterns) < 3 {
		t.Fatalf("expected at least 3 stats entries, got %d: %#v", len(stats.Patterns), stats.Patterns)
	}
	for i := 1; i < len(stats.Patterns); i++ {
		prev := stats.Patterns[i-1].PatternID
		curr := stats.Patterns[i].PatternID
		if prev >= curr {
			t.Errorf("Patterns[%d].PatternID=%q not strictly less than Patterns[%d].PatternID=%q",
				i-1, prev, i, curr)
		}
	}
}
