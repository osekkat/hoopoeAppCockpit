package redaction

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestRedactCanonicalSecretClasses(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := New(Config{Now: func() time.Time { return now }})
	input := strings.Join([]string{
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		"transport bearer Bearer abcdefghijklmnopqrstuvwxyz012345",
		"pairing H-ABCDEFGHJKM",
		"keys sk-abcdef0123456789ABCDEF0123456789 sk-ant-api03-abcdefABCDEF1234567890 AIzaSyA0123456789abcdefghijklmnopqrstuv AKIAIOSFODNN7EXAMPLE ghp_abcdefghijklmnopqrstuvwxyz0123456789 xoxb-123456789012-1234567890123-AbCdEfGhIjKl",
		"passphrase=hunter2",
		"Cookie: __Secure-next-auth.session-token=abc.def.ghi",
		"notify alice@example.com",
		"paths ~/.ssh/id_ed25519 /etc/shadow /private/var/db/login.keychain-db ~/.config/oracle/profile.json /home/ubuntu/Projects/hoopoeAppCockpit/.env /data/projects/hoopoe/.env",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
	}, "\n")

	out, traces := r.RedactText(SurfaceAudit, "audit.data.payload", input)

	for _, leak := range []string{
		"eyJhbGciOiJIUzI1NiJ9",
		"abcdefghijklmnopqrstuvwxyz012345",
		"H-ABCDEFGHJKM",
		"sk-abcdef",
		"sk-ant-api03",
		"AIzaSyA",
		"AKIAIOSF",
		"ghp_abc",
		"xoxb-123",
		"hunter2",
		"abc.def.ghi",
		"alice@example.com",
		".ssh/id_ed25519",
		"/etc/shadow",
		"/private/var/db",
		".config/oracle",
		"/home/ubuntu/Projects",
		"/data/projects/hoopoe",
		"BEGIN OPENSSH PRIVATE KEY",
	} {
		if strings.Contains(out, leak) {
			t.Fatalf("redacted output leaked %q in:\n%s", leak, out)
		}
	}
	if len(traces) < 10 {
		t.Fatalf("trace count = %d, want at least 10: %#v", len(traces), traces)
	}
	for _, trace := range traces {
		if !trace.Time.Equal(now) {
			t.Fatalf("trace time = %s, want %s", trace.Time, now)
		}
		if trace.Redactor != string(SurfaceAudit) {
			t.Fatalf("trace redactor = %q, want audit", trace.Redactor)
		}
		if trace.Context == "" || trace.PatternID == "" || trace.BytesRedacted == 0 || trace.Count == 0 {
			t.Fatalf("incomplete trace: %#v", trace)
		}
	}
}

func TestRedactValueWalksNestedJSONAndArrays(t *testing.T) {
	r := NewDefault()
	input := map[string]any{
		"command_preview": "GET /v1/jobs?token=sk-abcdef0123456789ABCDEF0123456789",
		"nested": map[string]any{
			"headers": []any{
				"Set-Cookie: oai_session=abcdef",
				map[string]any{"path": "/private/var/db/secret.db"},
			},
		},
		"safe": "unchanged",
	}

	out, traces := r.RedactValue(SurfaceEvents, "event.data", input)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out type = %T, want map", out)
	}
	if outMap["safe"] != "unchanged" {
		t.Fatalf("safe value mutated: %#v", outMap["safe"])
	}
	rendered := stringify(out)
	for _, leak := range []string{"sk-abcdef", "oai_session=abcdef", "/private/var/db"} {
		if strings.Contains(rendered, leak) {
			t.Fatalf("nested value leaked %q in %#v", leak, out)
		}
	}
	if len(traces) == 0 {
		t.Fatal("expected redaction traces")
	}
	foundContext := false
	for _, trace := range traces {
		if strings.Contains(trace.Context, "nested.headers") {
			foundContext = true
		}
	}
	if !foundContext {
		t.Fatalf("expected nested context in traces: %#v", traces)
	}
}

func TestRedactStackTraceAndBase64WrappedSecret(t *testing.T) {
	r := NewDefault()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	wrapped := base64.StdEncoding.EncodeToString([]byte("bearer=" + jwt))
	stack := "panic: auth failed\n\tmain.handleAuth\n\twrapped=" + wrapped

	out, traces := r.RedactText(SurfaceLogger, "log.fields.stack", stack)

	if strings.Contains(out, jwt) || strings.Contains(out, wrapped) {
		t.Fatalf("stack trace leaked secret:\n%s", out)
	}
	foundBase64 := false
	for _, trace := range traces {
		if trace.PatternID == "base64-wrapped-secret" {
			foundBase64 = true
			break
		}
	}
	if !foundBase64 {
		t.Fatalf("missing base64 trace: %#v", traces)
	}
}

func TestStatsAccumulateByPattern(t *testing.T) {
	r := NewDefault()
	_, _ = r.RedactText(SurfaceAudit, "audit.data", "sk-abcdef0123456789ABCDEF0123456789")
	_, _ = r.RedactText(SurfaceAudit, "audit.data", "sk-abcdef0123456789ABCDEF0123456789")

	stats := r.SnapshotStats()
	if stats.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", stats.SchemaVersion, SchemaVersion)
	}
	for _, stat := range stats.Patterns {
		if stat.PatternID == "provider-key-sk" && stat.Count == 2 && stat.BytesRedacted > 0 {
			return
		}
	}
	t.Fatalf("provider stats missing or wrong: %#v", stats.Patterns)
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case map[string]any:
		var b strings.Builder
		for key, child := range v {
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(stringify(child))
			b.WriteString(";")
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, child := range v {
			b.WriteString(stringify(child))
			b.WriteString(";")
		}
		return b.String()
	default:
		return ""
	}
}
