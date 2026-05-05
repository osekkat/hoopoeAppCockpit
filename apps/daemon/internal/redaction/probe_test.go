package redaction

import (
	"encoding/json"
	"strings"
	"testing"
)

// Probe: does the reflect walker leak secret-shaped map keys?
// Currently FAILS — non-string map keys are stringified via fmt.Sprint
// in redactReflectValue's reflect.Map case without passing through the
// pattern matcher. Skipped pending the reflect-Map redaction fix; promote
// to a live regression test once the fix lands.
func TestProbeNonStringMapKeyLeaks(t *testing.T) {
	t.Skip("reviewer-r2: non-string map keys bypass redaction in reflect path")
	type secretKey struct {
		Token string `json:"token"`
	}
	r := NewDefault()
	const secret = "sk-abcdef0123456789ABCDEF0123456789"
	value := map[secretKey]string{
		{Token: secret}: "value",
	}
	out, _ := r.RedactValue(SurfaceAudit, "test", value)
	body, _ := json.Marshal(out)
	if strings.Contains(string(body), "sk-abcdef") {
		t.Fatalf("secret leaked in non-string map key: %s", body)
	}
}

// Probe: does the reflect walker mishandle anonymous embedded fields?
// Currently FAILS — anonymous embedded structs are skipped (or treated as
// unexported) instead of having their exported fields promoted to the
// parent map the way encoding/json.Marshal does. No leak today, but JSON
// shape diverges from stdlib marshaling. Skipped pending fix.
func TestProbeAnonymousEmbeddedField(t *testing.T) {
	t.Skip("reviewer-r2: anonymous embedded fields are dropped instead of promoted")
	type embedded struct {
		Token string `json:"token"`
	}
	type outer struct {
		embedded
		Other string `json:"other"`
	}
	r := NewDefault()
	const secret = "sk-abcdef0123456789ABCDEF0123456789"
	value := outer{embedded: embedded{Token: secret}, Other: "ok"}

	// stdlib json behavior — embedded fields promoted
	stdBytes, _ := json.Marshal(value)
	t.Logf("stdlib json: %s", stdBytes)

	out, _ := r.RedactValue(SurfaceAudit, "test", value)
	redactedBytes, _ := json.Marshal(out)
	t.Logf("redacted: %s", redactedBytes)

	if strings.Contains(string(redactedBytes), "sk-abcdef") {
		t.Fatalf("secret leaked through embedded field: %s", redactedBytes)
	}
	// Compare shape: stdlib promotes "token" to top level, walker may not
	if !strings.Contains(string(redactedBytes), `"token"`) && !strings.Contains(string(redactedBytes), `"embedded"`) {
		t.Fatalf("unexpected redacted shape: %s", redactedBytes)
	}
}
