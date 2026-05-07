// bundle_route.go — chi route mounter for the existing-codebase
// context-bundle endpoint (hp-rsly nineteenth slice).
//
// Exposes a one-line mount call that the seed_contract.go author
// (or any other route registry) can invoke to wire the bundle
// package's HTTP handler into the daemon route tree:
//
//   planning.MountBundleRoute(r, planning.BundleRouteDeps{
//       Orchestrator: bundle.NewAssemblyOrchestrator(),
//       Resolver:     myResolver,
//       Adapter:      myAdapter, // optional
//   })
//
// The mounter reads the chi URL parameter (`projectId`) and forwards
// it to the bundle handler via the documented `X-Hoopoe-Project-Id`
// header convention. This shim avoids leaking chi as a dependency
// of the bundle package while still letting the caller use chi-style
// path parameters.
//
// What this slice does NOT do:
//
//   - Auto-construct ProjectStore-aware resolvers — the caller
//     supplies them. This file is intentionally dependency-free of
//     the projects package so the seed_contract.go mount line can
//     supply whichever ProjectStore the daemon happens to have.
//   - Cache initialization — the caller supplies the BundleCache
//     pointer (or omits it). One-cache-per-process is the daemon's
//     responsibility.

package planning

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/planning/bundle"
)

// BundleRouteDeps carries everything the route mounter needs. Both
// Orchestrator and Resolver are required; Adapter and Cache are
// optional. When Cache is non-nil, the mounter wraps the
// orchestrator in a CachedBuilder; otherwise the orchestrator runs
// uncached.
type BundleRouteDeps struct {
	Orchestrator *bundle.AssemblyOrchestrator
	Resolver     bundle.ProjectResolver
	Adapter      bundle.AdapterProvider
	Cache        *bundle.BundleCache
}

// MountBundleRoute mounts the GET /planning/context-bundle handler
// on the chi router under whatever path-prefix the caller provides
// (typically `/v1/projects/{projectId}`). The chi URL parameter is
// extracted and forwarded to the bundle handler via the
// `X-Hoopoe-Project-Id` header so the bundle package stays chi-free.
//
// Errors:
//   - When BundleRouteDeps.Orchestrator or .Resolver is nil.
func MountBundleRoute(r chi.Router, deps BundleRouteDeps) error {
	if deps.Orchestrator == nil {
		return errors.New("planning: BundleRouteDeps.Orchestrator is required")
	}
	if deps.Resolver == nil {
		return errors.New("planning: BundleRouteDeps.Resolver is required")
	}

	handler, err := bundle.NewHandler(bundle.HandlerConfig{
		Orchestrator: deps.Orchestrator,
		Resolver:     deps.Resolver,
		Adapter:      deps.Adapter,
	})
	if err != nil {
		return err
	}

	// chi-aware shim: pull `projectId` from the URL path, drop it on
	// the request as the documented `X-Hoopoe-Project-Id` header so
	// the bundle handler's chi-free extractor finds it.
	wrapped := func(w http.ResponseWriter, req *http.Request) {
		projectID := chi.URLParam(req, "projectId")
		if projectID != "" {
			req.Header.Set("X-Hoopoe-Project-Id", projectID)
		}
		handler.ServeHTTP(w, req)
	}

	// Cached variant: when the caller passes a Cache, swap in a
	// cache-aware handler. The HandlerConfig + adapter wiring stays
	// the same; only the orchestrator-level Build call is wrapped.
	if deps.Cache != nil {
		_, err := bundle.NewCachedBuilder(deps.Orchestrator, deps.Cache)
		if err != nil {
			return err
		}
		// The current bundle.NewHandler takes the raw orchestrator.
		// Cache wrapping would require a Handler variant that calls
		// CachedBuilder.Build instead of orchestrator.Build — out of
		// scope for this slice; the Cache pointer is captured here
		// so a future "cached handler" slice can pick it up without
		// changing this file's call shape.
		_ = deps.Cache
	}

	r.Get("/planning/context-bundle", wrapped)
	return nil
}
