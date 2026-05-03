package capabilities

import (
	"encoding/json"
	"net/http"
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
// current registry snapshot serialized as JSON. The handler refuses non-GET
// methods with 405; problem+json error envelopes (RFC 7807) come from the
// daemon's central error mapper (hp-g6sp), which wraps this handler.
func (r *Registry) HandleCapabilities(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := r.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

// HandleCompatibility is the GET /v1/compatibility handler. It composes the
// CompatibilityReport via the supplied composer and writes JSON. If composer
// returns nil (misconfiguration), the handler responds 500.
func (r *Registry) HandleCompatibility(composer CompatibilityComposer) http.HandlerFunc {
	if composer == nil {
		// Fail fast at wire-up; this isn't a runtime path.
		panic("capabilities: HandleCompatibility requires a non-nil composer")
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := r.Snapshot()
		report := composer.Compose(snap)
		if report == nil {
			http.Error(w, "compatibility composer returned nil", http.StatusInternalServerError)
			return
		}
		// Stamp schemaVersion + ensure capabilities is the live registry.
		report.SchemaVersion = SchemaVersion
		report.Capabilities = snap
		writeJSON(w, http.StatusOK, report)
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		// Encoding failure on a struct we control is a programmer error.
		// Surface it as 500 — tests catch this.
		http.Error(w, "internal encoding error", http.StatusInternalServerError)
	}
}
