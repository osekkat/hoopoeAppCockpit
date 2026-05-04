package capabilities

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// CompatibilityComposer assembles a CompatibilityReport for the daemon. It
// receives the registry snapshot and supplies the non-capability metadata
// (min-desktop, event schema versions, migration state). Implementations live
// in the daemon's HTTP wiring (hp-38d) — this package only knows about the
// shape and provides the shape-validating handler.
type CompatibilityComposer interface {
	Compose(registry *CapabilityRegistry) *CompatibilityReport
}

// HandleCapabilities is the GET /v1/capabilities handler. It returns the
// current registry snapshot serialized as JSON. Non-GET methods get a 405
// problem+json envelope; encoding failures get a 500 problem+json envelope
// (the body is buffered before WriteHeader so the failure can rewrite the
// status, per hp-49tc).
func (r *Registry) HandleCapabilities(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeCapabilityProblem(w, http.StatusMethodNotAllowed, "capabilities.method_not_allowed", "method not allowed", "GET /v1/capabilities is the only allowed method")
		return
	}
	snap := r.Snapshot()
	writeCapabilityJSON(w, http.StatusOK, snap)
}

// HandleCompatibility is the GET /v1/compatibility handler. It composes the
// CompatibilityReport via the supplied composer and writes JSON. If composer
// returns nil (misconfiguration), the handler responds 500 problem+json.
func (r *Registry) HandleCompatibility(composer CompatibilityComposer) http.HandlerFunc {
	if composer == nil {
		// Fail fast at wire-up; this isn't a runtime path.
		panic("capabilities: HandleCompatibility requires a non-nil composer")
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeCapabilityProblem(w, http.StatusMethodNotAllowed, "compatibility.method_not_allowed", "method not allowed", "GET /v1/compatibility is the only allowed method")
			return
		}
		snap := r.Snapshot()
		report := composer.Compose(snap)
		if report == nil {
			writeCapabilityProblem(w, http.StatusInternalServerError, "compatibility.composer_returned_nil", "compatibility composer returned nil", "the composer wired into the daemon returned nil — this is a misconfiguration")
			return
		}
		// Stamp schemaVersion + ensure capabilities is the live registry.
		report.SchemaVersion = SchemaVersion
		report.Capabilities = snap
		writeCapabilityJSON(w, http.StatusOK, report)
	}
}

// StaticCompatibilityComposer is a minimal CompatibilityComposer used by
// daemon boot when no richer composer has been wired yet (Phase 2 seed). It
// echoes the Registry's daemonAPIVersion plus a baseline migration state.
type StaticCompatibilityComposer struct {
	MinDesktopVersion   string
	EventSchemaVersions map[string]int
	Migration           MigrationState
}

func (s StaticCompatibilityComposer) Compose(registry *CapabilityRegistry) *CompatibilityReport {
	if registry == nil {
		return nil
	}
	eventVersions := s.EventSchemaVersions
	if eventVersions == nil {
		eventVersions = map[string]int{}
	}
	pending := s.Migration.Pending
	if pending == nil {
		pending = []string{}
	}
	return &CompatibilityReport{
		SchemaVersion:    SchemaVersion,
		DaemonAPIVersion: registry.DaemonAPIVersion,
		MinDesktopVersion: s.MinDesktopVersion,
		EventSchemaVersions: eventVersions,
		MigrationState: MigrationState{
			SchemaVersion: s.Migration.SchemaVersion,
			AppliedAt:     s.Migration.AppliedAt,
			Pending:       pending,
			Phase:         s.Migration.Phase,
		},
		Capabilities: registry,
	}
}

// writeCapabilityJSON encodes the payload into a buffer first, then writes
// the response. Buffering before WriteHeader is the fix for the original
// bug — encoding mid-stream (after WriteHeader committed) could not rewrite
// the status, so encoding failures shipped 200 with a truncated body.
func writeCapabilityJSON(w http.ResponseWriter, status int, payload any) {
	body, err := encodeCapabilityJSON(payload)
	if err != nil {
		writeCapabilityProblem(w, http.StatusInternalServerError, "daemon.encoding_failed", "internal encoding error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeCapabilityProblem emits an RFC 7807 problem+json envelope using the
// daemon-wide schemas.Problem shape so /v1/capabilities + /v1/compatibility
// match the rest of the daemon's error contract. Falls back to a hand-rolled
// envelope if schemas.Problem itself fails to encode (which would be a bug
// in the schema package).
func writeCapabilityProblem(w http.ResponseWriter, status int, code string, title string, detail string) {
	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	problem := schemas.Problem{
		Type:   "urn:hoopoe:problem:" + strings.ReplaceAll(code, ".", "-"),
		Title:  title,
		Status: status,
		Code:   code,
		Detail: detailPtr,
	}
	body, err := encodeCapabilityJSON(problem)
	if err != nil {
		body = []byte(`{"type":"urn:hoopoe:problem:daemon-encoding-failed","title":"internal encoding error","status":500,"code":"daemon.encoding_failed"}` + "\n")
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func encodeCapabilityJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}
