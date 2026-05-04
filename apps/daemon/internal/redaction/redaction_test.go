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

// TestRedactValueWalksTypedStruct verifies that EventHub-style payload
// structs (CommitCreatedPayload, ActivityData, etc.) get their string fields
// redacted. Without the reflect path RedactValue's default branch returned
// the struct verbatim, leaving secret-shaped commit messages and mail body
// previews on the WS/SSE wire.
func TestRedactValueWalksTypedStruct(t *testing.T) {
	type inner struct {
		Subject string `json:"subject"`
		Hidden  string `json:"-"`
	}
	type payload struct {
		ProjectID string    `json:"projectId"`
		Message   string    `json:"message"`
		Inner     *inner    `json:"inner,omitempty"`
		Tags      []string  `json:"tags,omitempty"`
		At        time.Time `json:"at"`
		Empty     string    `json:"empty,omitempty"`
	}

	r := NewDefault()
	now := time.Unix(1, 0).UTC()
	value := payload{
		ProjectID: "demo",
		Message:   "feat: rotate sk-abcdef0123456789ABCDEF0123456789",
		Inner:     &inner{Subject: "leaked: ghp_abcdefghijklmnopqrstuvwxyz0123456789", Hidden: "skipped"},
		Tags:      []string{"normal", "Bearer abcdefghijklmnopqrstuvwxyz012345"},
		At:        now,
	}

	out, traces := r.RedactValue(SurfaceEvents, "event.data", value)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out type = %T, want map[string]any", out)
	}
	if outMap["projectId"] != "demo" {
		t.Fatalf("projectId mutated: %#v", outMap["projectId"])
	}
	msg, ok := outMap["message"].(string)
	if !ok {
		t.Fatalf("message type = %T, want string", outMap["message"])
	}
	if strings.Contains(msg, "sk-abcdef") {
		t.Fatalf("message leaked secret: %q", msg)
	}
	innerMap, ok := outMap["inner"].(map[string]any)
	if !ok {
		t.Fatalf("inner type = %T, want map[string]any", outMap["inner"])
	}
	if subject, _ := innerMap["subject"].(string); strings.Contains(subject, "ghp_") {
		t.Fatalf("inner.subject leaked secret: %q", subject)
	}
	if _, present := innerMap["hidden"]; present {
		t.Fatal(`json:"-" field "hidden" should be skipped`)
	}
	tags, ok := outMap["tags"].([]any)
	if !ok {
		t.Fatalf("tags type = %T, want []any", outMap["tags"])
	}
	if len(tags) != 2 {
		t.Fatalf("tags length = %d, want 2", len(tags))
	}
	if tag, _ := tags[1].(string); strings.Contains(tag, "abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("tags[1] leaked secret: %q", tag)
	}
	at, ok := outMap["at"].(time.Time)
	if !ok || !at.Equal(now) {
		t.Fatalf("time.Time mangled: %#v", outMap["at"])
	}
	if _, present := outMap["empty"]; present {
		t.Fatal("omitempty zero-value should be skipped")
	}
	if len(traces) == 0 {
		t.Fatal("expected traces from struct walk")
	}
	foundInnerCtx := false
	for _, trace := range traces {
		if strings.HasPrefix(trace.Context, "event.data.inner.subject") {
			foundInnerCtx = true
		}
	}
	if !foundInnerCtx {
		t.Fatalf("expected inner.subject trace context, traces: %#v", traces)
	}
}

// TestRedactValueWalksNilPointersAndSlices verifies the reflect path handles
// nil-valued fields without panicking and without producing false-positive
// "redacted" markers.
func TestRedactValueWalksNilPointersAndSlices(t *testing.T) {
	type payload struct {
		Optional *string  `json:"optional,omitempty"`
		List     []string `json:"list,omitempty"`
		Empty    *struct {
			Inner string `json:"inner"`
		} `json:"empty,omitempty"`
	}
	r := NewDefault()
	out, _ := r.RedactValue(SurfaceEvents, "event.data", payload{})
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out type = %T, want map[string]any", out)
	}
	if len(outMap) != 0 {
		t.Fatalf("expected empty output (omitempty), got %#v", outMap)
	}
}

// TestRedactValueWalksSliceOfStructs covers []TypedStruct values that flow
// through producers like origin_updated payloads and Agent Mail recipient
// lists.
func TestRedactValueWalksSliceOfStructs(t *testing.T) {
	type ref struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	type payload struct {
		Refs []ref `json:"refs"`
	}
	r := NewDefault()
	value := payload{
		Refs: []ref{
			{Name: "main", Note: "see Bearer abcdefghijklmnopqrstuvwxyz012345"},
			{Name: "feature", Note: "clean"},
		},
	}
	out, _ := r.RedactValue(SurfaceEvents, "event.data", value)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out type = %T", out)
	}
	refs, ok := outMap["refs"].([]any)
	if !ok {
		t.Fatalf("refs type = %T", outMap["refs"])
	}
	if len(refs) != 2 {
		t.Fatalf("refs length = %d, want 2", len(refs))
	}
	first, ok := refs[0].(map[string]any)
	if !ok {
		t.Fatalf("refs[0] type = %T", refs[0])
	}
	if note, _ := first["note"].(string); strings.Contains(note, "abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("refs[0].note leaked: %q", note)
	}
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
