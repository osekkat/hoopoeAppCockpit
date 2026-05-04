// Package api — capability + compatibility routes for the production
// daemon (hp-4vk wiring of hp-r33's registry).
//
// Mounted by `NewRouter` once a `*capabilities.Registry` is supplied via
// `Config.Capabilities`. Without it, the routes 503 with a problem+json
// body so callers see a deterministic error instead of a 404 (the hp-r3i
// /v1/capabilities contract is "always available; degrades when probes
// fail" — never absent).
//
// Cross-references:
//   - plan.md §2.6 (seed daemon API), §2.8 (capability registry shape).
//   - apps/daemon/internal/capabilities/ — the registry primitive +
//     HandleCapabilities / HandleCompatibility implementations.
//   - apps/daemon/internal/mock/daemon.go — mock daemon mounts the same
//     two routes; this file mirrors that wiring for the real daemon.

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

// mountCapabilityRoutes attaches /v1/capabilities and /v1/compatibility to
// the given router. If the registry is nil, both routes still respond — but
// with 503 and a problem+json envelope explaining the daemon was started
// without a configured registry. That's a misconfiguration, not a runtime
// error; callers must surface it in Diagnostics.
func (s *server) mountCapabilityRoutes(r chi.Router) {
	if s.capabilities == nil {
		r.Get("/v1/capabilities", s.unconfiguredCapabilitiesHandler())
		r.Get("/v1/compatibility", s.unconfiguredCapabilitiesHandler())
		return
	}
	r.Get("/v1/capabilities", s.capabilities.HandleCapabilities)
	r.Get("/v1/compatibility", s.capabilities.HandleCompatibility(s.compatibilityComposer()))
}

// compatibilityComposer wires the daemon's BuildInfo + event schema
// versions + (placeholder) migration state into a CompatibilityReport
// composer. Phase 2.5+ replaces this with a richer composer that reads
// migration state from SQLite + min-desktop from the release manifest
// (hp-2eg6 deliverable).
func (s *server) compatibilityComposer() capabilities.CompatibilityComposer {
	now := s.now().UTC().Format(timeFormatRFC3339)
	return capabilities.StaticCompatibilityComposer{
		MinDesktopVersion: "0.0.0",
		EventSchemaVersions: map[string]int{
			"_system": schemaVersion,
			"project": schemaVersion,
			"swarm":   schemaVersion,
		},
		Migration: capabilities.MigrationState{
			SchemaVersion: schemaVersion,
			AppliedAt:     now,
			Pending:       []string{},
			Phase:         capabilities.MigrationIdle,
		},
	}
}

// unconfiguredCapabilitiesHandler returns a 503 problem+json envelope when
// the daemon's Config.Capabilities is nil. Surfaces a deterministic error
// for the renderer's Diagnostics page rather than a generic 404.
func (s *server) unconfiguredCapabilitiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(
			w,
			http.StatusServiceUnavailable,
			"capability registry not wired",
			"the daemon was started without a Config.Capabilities; /v1/capabilities and /v1/compatibility cannot answer",
		)
	}
}

// timeFormatRFC3339 is the wire format used by capability snapshots. The
// constant is local to the api package so the import surface stays
// minimal.
const timeFormatRFC3339 = "2006-01-02T15:04:05Z07:00"
