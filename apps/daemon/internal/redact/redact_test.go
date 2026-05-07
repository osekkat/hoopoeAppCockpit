//go:build hoopoe_legacy_redact

// hp-mdex: see redact.go header — these tests only build under the
// `hoopoe_legacy_redact` tag. Default `go test ./...` skips them so
// the legacy package does not participate in production builds.

package redact

import (
	"strings"
	"testing"
)

// Pattern coverage tests — at least one positive case per pattern ID.

func TestPrivateKeyBlock(t *testing.T) {
	r := New()
	for _, kind := range []string{"RSA", "OPENSSH", "EC", "DSA", "PGP"} {
		input := "loading: -----BEGIN " + kind + " PRIVATE KEY-----\nbody\n-----END " + kind + " PRIVATE KEY-----\nok"
		if kind == "PGP" {
			input = "loading: -----BEGIN " + kind + " PRIVATE KEY BLOCK-----\nbody\n-----END " + kind + " PRIVATE KEY BLOCK-----\nok"
		}
		out, events := r.Redact(input)
		if strings.Contains(out, "BEGIN") {
			t.Errorf("%s key block not redacted: %s", kind, out)
		}
		if !strings.Contains(out, "[private-key-redacted]") {
			t.Errorf("%s expected placeholder", kind)
		}
		if len(events) != 1 || events[0].Pattern != "private-key-block" {
			t.Errorf("%s expected private-key-block event", kind)
		}
	}
}

func TestBearerHMAC(t *testing.T) {
	r := New()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	out, events := r.Redact("Bearer " + jwt)
	if strings.Contains(out, jwt) {
		t.Errorf("JWT not redacted")
	}
	if !strings.Contains(out, "sha256:") {
		t.Errorf("expected sha256 tag")
	}
	if events[0].Pattern != "bearer-hmac" {
		t.Errorf("got %v", events[0].Pattern)
	}
}

func TestProviderKeys(t *testing.T) {
	r := New()
	cases := []struct {
		name, input, pattern string
	}{
		{"openai", "key=sk-abcdef0123456789ABCDEF0123456789", "provider-key-openai"},
		{"anthropic", "key=sk-ant-api03-abcdefABCDEF1234567890", "provider-key-anthropic"},
		{"google", "key=AIzaSyA0123456789abcdefghijklmnopqrstuv", "provider-key-google"},
		{"aws", "key=AKIAIOSFODNN7EXAMPLE", "provider-key-aws"},
		{"github", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "provider-key-github"},
		{"slack", "xoxb-123456789012-1234567890123-AbCdEfGhIjKl", "provider-key-slack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, events := r.Redact(tc.input)
			if out == tc.input {
				t.Errorf("not redacted: %s", out)
			}
			found := false
			for _, e := range events {
				if e.Pattern == tc.pattern {
					found = true
				}
			}
			if !found {
				t.Errorf("missing pattern %s in %v", tc.pattern, events)
			}
		})
	}
}

func TestPairingToken(t *testing.T) {
	r := New()
	out, events := r.Redact("paired with H-ABCDEFGHJKM in 5 sec")
	if !strings.Contains(out, "[pairing-token-redacted]") {
		t.Errorf("not redacted: %s", out)
	}
	if events[0].Pattern != "pairing-token" {
		t.Errorf("got %v", events[0].Pattern)
	}
}

func TestHTTPAuthorizationHeader(t *testing.T) {
	r := New()
	out, events := r.Redact("Authorization: Bearer sk-abcdef0123456789ABCDEF0123456789")
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("auth header leaked: %s", out)
	}
	// Should be replaced with placeholder. Either http-authorization-header
	// fires OR the bearer/provider-key fires + then header fires.
	hit := false
	for _, e := range events {
		if e.Pattern == "http-authorization-header" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected http-authorization-header event, got %+v", events)
	}
}

func TestHTTPCookieHeaders(t *testing.T) {
	r := New()
	out, _ := r.Redact("Set-Cookie: session=abcdefghijklmnop; Path=/")
	if strings.Contains(out, "abcdefghijklmnop") {
		t.Errorf("set-cookie header leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted-header]") {
		t.Errorf("expected placeholder: %s", out)
	}
	out2, _ := r.Redact("Cookie: session=abcdefghijklmnop")
	if strings.Contains(out2, "abcdefghijklmnop") {
		t.Errorf("cookie header leaked: %s", out2)
	}
}

func TestSSHPassphrase(t *testing.T) {
	r := New()
	out, events := r.Redact("Passphrase=hunter2")
	if strings.Contains(out, "hunter2") {
		t.Errorf("passphrase leaked")
	}
	if events[0].Pattern != "ssh-passphrase" {
		t.Errorf("got %v", events[0].Pattern)
	}
}

func TestProviderCookies(t *testing.T) {
	r := New()
	for _, in := range []string{
		"__Secure-next-auth.session-token=abc.def.ghi",
		"sessionKey=sk-ant-api03-abc",
		"__Host-next-auth.csrf-token=abc%2Bdef.ghi",
	} {
		out, events := r.Redact(in)
		if out == in {
			t.Errorf("not redacted: %s", in)
		}
		if len(events) == 0 {
			t.Errorf("no events fired for %s", in)
		}
	}
}

func TestTelegramBotToken(t *testing.T) {
	r := New()
	out, _ := r.Redact("token=123456789:AAEhBP0avQ7AdEXAMPLE_THISisJUST_a_PLACEHOLDER")
	if strings.Contains(out, "123456789:AAE") {
		t.Errorf("telegram token leaked")
	}
}

func TestEmailLast4(t *testing.T) {
	r := New()
	out, _ := r.Redact("notify alice@example.com please")
	if strings.Contains(out, "alice@example.com") {
		t.Errorf("email leaked")
	}
	if !strings.Contains(out, "@example.com") {
		t.Errorf("expected last-4 + domain")
	}
}

func TestSensitiveFilePaths(t *testing.T) {
	r := New()
	for _, in := range []string{
		"reading ~/.ssh/id_rsa",
		"shadow at /etc/shadow",
		"keychain at /private/var/db/login.keychain-db",
		"loaded ~/.config/oracle/profiles/main",
	} {
		out, events := r.Redact(in)
		if out == in {
			t.Errorf("not redacted: %s", in)
		}
		if len(events) == 0 {
			t.Errorf("no events: %s", in)
		}
	}
}

// Adversarial: nested in JSON.
func TestAdversarial_NestedJSON(t *testing.T) {
	r := New()
	out, _ := r.Redact(`{"args":{"key":"sk-abcdef0123456789ABCDEF0123456789"}}`)
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("not redacted: %s", out)
	}
}

// Adversarial: URL query.
func TestAdversarial_URLQuery(t *testing.T) {
	r := New()
	out, _ := r.Redact("GET /v1/jobs?token=sk-abcdef0123456789ABCDEF0123456789&action=run")
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("not redacted: %s", out)
	}
}

// Adversarial: stack trace.
func TestAdversarial_StackTrace(t *testing.T) {
	r := New()
	stack := `panic: runtime error
goroutine 1 [running]:
main.handleAuth(0xc0000a0000)
	/build/auth.go:42 +0xff
		bearer=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`
	out, events := r.Redact(stack)
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("JWT in stack trace not redacted")
	}
	hasBearer := false
	for _, e := range events {
		if e.Pattern == "bearer-hmac" {
			hasBearer = true
		}
	}
	if !hasBearer {
		t.Errorf("expected bearer-hmac event")
	}
}

// Adversarial: multi-line value (e.g., heredoc payload).
func TestAdversarial_MultiLineValue(t *testing.T) {
	r := New()
	input := `payload <<EOF
line1
sk-abcdef0123456789ABCDEF0123456789
line3
EOF`
	out, _ := r.Redact(input)
	if strings.Contains(out, "sk-abcdef") {
		t.Errorf("multi-line secret leaked: %s", out)
	}
}

// Adversarial: base64-wrapped (e.g., a credentials blob inside base64).
// Since our patterns target unencoded plaintext, base64-wrapped secrets are
// expected to *survive* — this test pins that limitation explicitly so a
// future contributor knows it's a known gap to handle at the audit-log
// boundary. If this test ever needs to flip, do it deliberately.
func TestAdversarial_Base64Wrapped(t *testing.T) {
	r := New()
	// "sk-abcdef0123456789ABCDEF0123456789" base64-encoded:
	encoded := "c2stYWJjZGVmMDEyMzQ1Njc4OUFCQ0RFRjAxMjM0NTY3ODk="
	input := "blob=" + encoded
	out, _ := r.Redact(input)
	// Expected: NOT redacted (the secret is opaque inside base64). Caller
	// must base64-decode before logging if they want plaintext redaction.
	if !strings.Contains(out, encoded) {
		t.Errorf("base64-wrapped value should pass through unchanged: %s", out)
	}
}

// Multiple secrets in one input.
func TestMultipleSecretsOneInput(t *testing.T) {
	r := New()
	input := "k1=sk-abcdef0123456789ABCDEF0123456789 k2=AKIAIOSFODNN7EXAMPLE k3=ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	out, events := r.Redact(input)
	for _, leak := range []string{"sk-abcdef", "AKIAIOSF", "ghp_abc"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s leaked", leak)
		}
	}
	if len(events) < 3 {
		t.Errorf("expected ≥3 events, got %d", len(events))
	}
}

// RedactValue walks nested maps + arrays.
func TestRedactValue_Recursion(t *testing.T) {
	r := New()
	input := map[string]any{
		"outer": map[string]any{
			"key": "sk-abcdef0123456789ABCDEF0123456789",
			"arr": []any{"AKIAIOSFODNN7EXAMPLE", "no secret"},
		},
	}
	out, events := r.RedactValue(input)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("not a map")
	}
	inner, ok := outMap["outer"].(map[string]any)
	if !ok {
		t.Fatalf("not nested")
	}
	if v, _ := inner["key"].(string); strings.Contains(v, "sk-abcdef") {
		t.Errorf("nested key leaked")
	}
	arr, _ := inner["arr"].([]any)
	if v, _ := arr[0].(string); strings.Contains(v, "AKIAIOSF") {
		t.Errorf("nested array leaked")
	}
	if len(events) == 0 {
		t.Errorf("no events from nested")
	}
}

// PatternIDs is stable + complete.
func TestPatternIDs(t *testing.T) {
	r := New()
	ids := r.PatternIDs()
	want := []string{
		"private-key-block",
		"bearer-hmac",
		"provider-key-anthropic",
		"provider-key-openai",
		"provider-key-google",
		"provider-key-aws",
		"provider-key-github",
		"provider-key-slack",
		"pairing-token",
		"http-authorization-header",
		"http-cookie-header",
		"http-cookie-header-request",
		"ssh-passphrase",
		"chatgpt-cookie",
		"claude-ai-cookie",
		"openai-com-cookie",
		"telegram-bot-token",
		"email-address",
		"ssh-key-path",
		"shadow-file-path",
		"macos-keychain-path",
		"oracle-profile-path",
	}
	if len(ids) != len(want) {
		t.Fatalf("got %d ids want %d", len(ids), len(want))
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d]=%s want %s (order matters)", i, ids[i], want[i])
		}
	}
}

func TestEvent_BytesRedacted(t *testing.T) {
	r := New()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	_, events := r.Redact(jwt)
	if events[0].BytesRedacted != len(jwt) {
		t.Errorf("expected bytesRedacted=%d, got %d", len(jwt), events[0].BytesRedacted)
	}
}

func TestEmpty(t *testing.T) {
	r := New()
	out, events := r.Redact("")
	if out != "" {
		t.Errorf("got %q", out)
	}
	if len(events) != 0 {
		t.Errorf("got events: %v", events)
	}
}

// Stats accumulator.
func TestStats(t *testing.T) {
	stats := NewStats()
	stats.Record([]Event{{Pattern: "bearer-hmac", Count: 2, BytesRedacted: 100}})
	stats.Record([]Event{{Pattern: "bearer-hmac", Count: 1, BytesRedacted: 50}})
	stats.Record([]Event{{Pattern: "pairing-token", Count: 3, BytesRedacted: 36}})

	counts, bytes := stats.Snapshot()
	if counts["bearer-hmac"] != 3 {
		t.Errorf("bearer-hmac count=%d", counts["bearer-hmac"])
	}
	if bytes["bearer-hmac"] != 150 {
		t.Errorf("bearer-hmac bytes=%d", bytes["bearer-hmac"])
	}
	if counts["pairing-token"] != 3 {
		t.Errorf("pairing-token count=%d", counts["pairing-token"])
	}
	if bytes["pairing-token"] != 36 {
		t.Errorf("pairing-token bytes=%d", bytes["pairing-token"])
	}
}

func TestStats_Concurrent(t *testing.T) {
	stats := NewStats()
	const goroutines = 8
	const perGoroutine = 50
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < perGoroutine; j++ {
				stats.Record([]Event{{Pattern: "bearer-hmac", Count: 1, BytesRedacted: 10}})
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	counts, _ := stats.Snapshot()
	if counts["bearer-hmac"] != goroutines*perGoroutine {
		t.Errorf("expected %d, got %d", goroutines*perGoroutine, counts["bearer-hmac"])
	}
}

func TestStats_NilSafe(t *testing.T) {
	var s *Stats
	s.Record([]Event{{Pattern: "x", Count: 1}})
	counts, bytes := s.Snapshot()
	if len(counts) != 0 || len(bytes) != 0 {
		t.Errorf("nil Stats should yield empty maps")
	}
}
