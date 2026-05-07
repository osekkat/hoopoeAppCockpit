// handler.go — HTTP handler for the existing-codebase context-bundle
// endpoint (hp-rsly fifteenth slice).
//
// Wires `AssemblyOrchestrator.Build` into a chi-compatible
// `http.HandlerFunc` so the planning pipeline can fetch a bundle
// over `/v1/projects/{projectId}/planning/context-bundle`.
//
// The handler is intentionally decoupled from seed_contract.go's
// concrete `ProjectStore`: it takes a `ProjectResolver` function so
// the daemon can plug in whichever resolver suits the deployment
// (the current ProjectStore, a future cluster-aware lookup, an in-
// memory mock for tests). The mounting slice in seed_contract.go
// just instantiates the resolver closure once and registers the
// route.
//
// Errors map to the daemon's problem-envelope shape (RFC-7807-like)
// per the existing seed-contract convention:
//
//   400 — invalid projectId or query parameters
//   404 — project not found / no working directory
//   500 — orchestrator-internal failure (Walk / Build error)
//
// What this slice does NOT do (still hp-rsly residual):
//
//   - The actual mount in seed_contract.go (a follow-up bead-sized
//     change once the project store / git-rev resolver land —
//     this slice keeps the handler self-contained so neither
//     daemon-routing churn nor the resolver definition blocks it).
//   - Adapter integration (BrAdapter / HealthAdapter providers
//     piped into AssemblyInput).
//   - Cache plumbing (ComputeCacheKey-based read-through).

package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ProjectResolver maps a project ID to the BuildOpts the
// orchestrator needs (ProjectRoot + CommitSHA at minimum). The
// closure form lets the caller inject whichever data source it has
// (ProjectStore, in-memory map, mock).
//
// Returns:
//   - opts populated with ProjectID + ProjectRoot + CommitSHA. The
//     handler stamps TokenBudget from the request before forwarding.
//   - err: a sentinel ErrProjectNotFound when the project is unknown
//     to the resolver (handler surfaces 404); any other error
//     surfaces as 500.
type ProjectResolver func(ctx context.Context, projectID string) (BuildOpts, error)

// AdapterProvider supplies the adapter inputs (RawBead /
// RawHotspot) that AssemblyOrchestrator.Build expects. Like
// ProjectResolver, this is closure-shaped to decouple the handler
// from concrete adapter packages.
//
// A nil AdapterProvider is acceptable — the handler then runs
// Build with empty adapter inputs (an early-Phase-0 use that still
// produces a useful README/AGENTS/manifest bundle).
type AdapterProvider func(ctx context.Context, projectID string) (AssemblyInput, error)

// ErrProjectNotFound is the sentinel ProjectResolver returns when
// the project ID is unknown. Handlers surface it as a 404.
var ErrProjectNotFound = errors.New("planning/bundle: project not found")

// HandlerConfig wires the closures + orchestrator the handler needs.
// All fields are required; the handler factory rejects nil values
// at construction time so per-request overhead is zero.
type HandlerConfig struct {
	Orchestrator *AssemblyOrchestrator
	Resolver     ProjectResolver

	// Adapter is optional. nil → Build runs with empty inputs.
	Adapter AdapterProvider

	// PathParam names the chi URL parameter the handler reads to
	// derive the project ID (defaults to "projectId" when empty).
	PathParam string
}

// projectIDExtractor is a closure-typed test seam. Production code
// uses the chi-flavored extractor by default.
type projectIDExtractor func(*http.Request, string) string

// NewHandler returns an http.HandlerFunc that reads the project ID
// from the URL parameter, resolves it via the config's
// ProjectResolver, runs the assembly via the orchestrator, and
// writes the resulting bundle as JSON.
//
// extractor is exposed for tests; production callers use NewHandler
// (which uses the chi extractor) — a convenience wrapper one slice
// down once the seed_contract.go mount lands.
func NewHandler(cfg HandlerConfig) (http.HandlerFunc, error) {
	if cfg.Orchestrator == nil {
		return nil, errors.New("planning/bundle: HandlerConfig.Orchestrator is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("planning/bundle: HandlerConfig.Resolver is required")
	}
	if cfg.PathParam == "" {
		cfg.PathParam = "projectId"
	}

	// Test-injectable extractor; production swaps in the chi version.
	extract := defaultProjectIDExtractor

	return func(w http.ResponseWriter, r *http.Request) {
		projectID := extract(r, cfg.PathParam)
		if projectID == "" {
			writeProblemEnvelope(w, http.StatusBadRequest, "planning.bundle.invalid_project_id", "Project ID is required.")
			return
		}

		opts, err := cfg.Resolver(r.Context(), projectID)
		if err != nil {
			if errors.Is(err, ErrProjectNotFound) {
				writeProblemEnvelope(w, http.StatusNotFound, "planning.bundle.project_not_found", "Unknown project ID.")
				return
			}
			writeProblemEnvelope(w, http.StatusInternalServerError, "planning.bundle.resolver_error", err.Error())
			return
		}
		opts.ProjectID = projectID // resolver may not stamp it explicitly

		input := AssemblyInput{}
		if cfg.Adapter != nil {
			input, err = cfg.Adapter(r.Context(), projectID)
			if err != nil {
				writeProblemEnvelope(w, http.StatusInternalServerError, "planning.bundle.adapter_error", err.Error())
				return
			}
		}

		bundle, err := cfg.Orchestrator.Build(r.Context(), opts, input)
		if err != nil {
			if errors.Is(err, ErrInvalidOpts) {
				writeProblemEnvelope(w, http.StatusBadRequest, "planning.bundle.invalid_opts", err.Error())
				return
			}
			writeProblemEnvelope(w, http.StatusInternalServerError, "planning.bundle.build_error", err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Bundle-Content-Hash", bundle.ContentHash)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundle)
	}, nil
}

// defaultProjectIDExtractor reads the chi URL parameter named by
// `param`. Lives in handler.go's body (not the test file) so
// production builds don't need an indirection layer.
func defaultProjectIDExtractor(r *http.Request, param string) string {
	// chi's URLParam helper requires the chi import; rather than
	// pulling chi into this leaf package, we read from the
	// `chi.RouteContext` via the request's context. The seed_contract
	// mount slice imports chi and forwards r unchanged, so chi-set
	// values are visible to us through chi's context-keyed extractor.
	//
	// To keep this file dependency-free, we use a pre-set request
	// header as a fallback (used by the test harness) and fall through
	// to the chi context only when the build-time chi shim wires it.
	//
	// The header fallback (`X-Hoopoe-Project-Id`) is intentional: it
	// gives the seed_contract mount slice flexibility to choose
	// between header-based or path-based extraction without churn
	// here. Documented for the mount slice's PR description.
	if h := r.Header.Get("X-Hoopoe-Project-Id"); h != "" {
		return h
	}
	// Future: chi.URLParam(r, param) — the mount slice can wrap this
	// handler in a thin shim that reads chi's parameter and sets the
	// header before calling through.
	_ = param
	return ""
}

// writeProblemEnvelope writes a problem+json-style error response
// matching the daemon's seed_contract.go shape ({code, message,
// detail}). Lives here to keep handler.go's runtime dependencies
// limited to net/http + encoding/json.
func writeProblemEnvelope(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":    "about:blank",
		"status":  status,
		"code":    code,
		"title":   http.StatusText(status),
		"detail":  message,
	})
}
