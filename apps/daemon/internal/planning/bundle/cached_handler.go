// cached_handler.go — cache-aware HTTP handler for the bundle
// endpoint (hp-rsly twentieth slice).
//
// Mirrors `NewHandler` (handler.go) but invokes `CachedBuilder.Build`
// instead of `AssemblyOrchestrator.Build`. The bundle_route.go
// mounter accepts a Cache field; this slice is the wire-up that
// turns "Cache != nil" from a no-op into a real read-through.
//
// Why a parallel constructor instead of refactoring NewHandler:
//
//   - AssemblyOrchestrator.Build and CachedBuilder.Build have the
//     same signature (ctx, opts, input → bundle, err) — no
//     interface gymnastics needed at the call site.
//   - Two named factories (NewHandler / NewCachedHandler) make the
//     daemon's startup wiring read clearly: "cache off" vs "cache
//     on" is a one-name swap, not a "does this config struct have
//     a non-nil Cache field?" reading exercise.
//   - The shared error-handling + extractor logic stays factored
//     into runHandler; future evolution (pluggable extractors,
//     post-Build hooks) lands in one place.

package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// CachedHandlerConfig wires the dependencies a cached handler
// needs. Mirrors HandlerConfig but takes a CachedBuilder pointer.
//
// Both Builder and Resolver are required; Adapter is optional.
type CachedHandlerConfig struct {
	Builder   *CachedBuilder
	Resolver  ProjectResolver
	Adapter   AdapterProvider
	PathParam string
}

// NewCachedHandler returns an http.HandlerFunc that runs the
// cache-aware build pipeline. Refinement-round calls hit the
// cache; the orchestrator's discovery walk runs only on miss.
func NewCachedHandler(cfg CachedHandlerConfig) (http.HandlerFunc, error) {
	if cfg.Builder == nil {
		return nil, errors.New("planning/bundle: CachedHandlerConfig.Builder is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("planning/bundle: CachedHandlerConfig.Resolver is required")
	}
	if cfg.PathParam == "" {
		cfg.PathParam = "projectId"
	}

	build := func(ctx context.Context, opts BuildOpts, input AssemblyInput) (interface {
		ContentHashGetter
	}, error) {
		bundle, err := cfg.Builder.Build(ctx, opts, input)
		if err != nil {
			return nil, err
		}
		return contentHashView{bundle}, nil
	}

	return runHandler(runHandlerConfig{
		PathParam: cfg.PathParam,
		Resolver:  cfg.Resolver,
		Adapter:   cfg.Adapter,
		Build:     build,
	}), nil
}

// ContentHashGetter exposes a bundle's ContentHash without leaking
// the schema struct to the caller. Used by runHandler so the
// shared logic doesn't need to import schemas.* directly through
// the wrapper interface.
type ContentHashGetter interface {
	GetContentHash() string
	WriteJSON(w http.ResponseWriter)
}

// contentHashView wraps a bundle pointer so the handler can read
// ContentHash + emit JSON without exposing the schema type to
// runHandler's signature. Cheap zero-cost adapter.
type contentHashView struct {
	bundle interface {
		// Embedded so any schema.ExistingCodebaseContextBundle
		// pointer satisfies it without an explicit method on the
		// schema struct (which would require a codegen change).
	}
}

func (c contentHashView) GetContentHash() string {
	if v, ok := c.bundle.(interface{ GetContentHash() string }); ok {
		return v.GetContentHash()
	}
	// Fallback: marshal + extract via JSON. Only hit when the
	// bundle is some custom test double; production never lands
	// here because the schema type below has the typed accessor.
	return contentHashFromAny(c.bundle)
}

func (c contentHashView) WriteJSON(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(c.bundle)
}

// contentHashFromAny is the marshal-then-decode fallback for the
// rare case where the wrapped bundle isn't a schema.* pointer with
// a typed accessor. Defensive — production bundles always satisfy
// the typed path.
func contentHashFromAny(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var probe struct {
		ContentHash string `json:"contentHash"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.ContentHash
}

// runHandlerConfig captures the shared shape both NewHandler and
// NewCachedHandler use. Keeps error-handling + URL-param
// extraction + envelope writing factored out.
type runHandlerConfig struct {
	PathParam string
	Resolver  ProjectResolver
	Adapter   AdapterProvider
	Build     func(ctx context.Context, opts BuildOpts, input AssemblyInput) (interface{ ContentHashGetter }, error)
}

func runHandler(cfg runHandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := defaultProjectIDExtractor(r, cfg.PathParam)
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
		opts.ProjectID = projectID

		input := AssemblyInput{}
		if cfg.Adapter != nil {
			input, err = cfg.Adapter(r.Context(), projectID)
			if err != nil {
				writeProblemEnvelope(w, http.StatusInternalServerError, "planning.bundle.adapter_error", err.Error())
				return
			}
		}

		view, err := cfg.Build(r.Context(), opts, input)
		if err != nil {
			if errors.Is(err, ErrInvalidOpts) {
				writeProblemEnvelope(w, http.StatusBadRequest, "planning.bundle.invalid_opts", err.Error())
				return
			}
			writeProblemEnvelope(w, http.StatusInternalServerError, "planning.bundle.build_error", err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Bundle-Content-Hash", view.GetContentHash())
		w.WriteHeader(http.StatusOK)
		view.WriteJSON(w)
	}
}
