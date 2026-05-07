package planning

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/planning/bundle"
)

func setupRouteProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(rel string, content []byte) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	must("README.md", []byte("# Demo"))
	must("package.json", []byte(`{"name":"x"}`))
	return root
}

func TestMountBundleRouteRejectsNilOrchestrator(t *testing.T) {
	r := chi.NewRouter()
	err := MountBundleRoute(r, BundleRouteDeps{
		Resolver: func(context.Context, string) (bundle.BuildOpts, error) {
			return bundle.BuildOpts{}, nil
		},
	})
	if err == nil {
		t.Fatal("nil orchestrator should error")
	}
}

func TestMountBundleRouteRejectsNilResolver(t *testing.T) {
	r := chi.NewRouter()
	err := MountBundleRoute(r, BundleRouteDeps{
		Orchestrator: bundle.NewAssemblyOrchestrator(),
	})
	if err == nil {
		t.Fatal("nil resolver should error")
	}
}

func TestMountBundleRouteEndToEnd(t *testing.T) {
	root := setupRouteProject(t)
	r := chi.NewRouter()
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		err := MountBundleRoute(r, BundleRouteDeps{
			Orchestrator: bundle.NewAssemblyOrchestrator(),
			Resolver: func(ctx context.Context, id string) (bundle.BuildOpts, error) {
				return bundle.BuildOpts{
					ProjectID:   id,
					ProjectRoot: root,
					CommitSHA:   strings.Repeat("a", 40),
					TokenBudget: 100_000,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("MountBundleRoute: %v", err)
		}
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/projects/demo/planning/context-bundle")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Bundle-Content-Hash") == "" {
		t.Error("X-Bundle-Content-Hash header not set")
	}
}

func TestMountBundleRouteProjectNotFoundReturns404(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		_ = MountBundleRoute(r, BundleRouteDeps{
			Orchestrator: bundle.NewAssemblyOrchestrator(),
			Resolver: func(ctx context.Context, id string) (bundle.BuildOpts, error) {
				return bundle.BuildOpts{}, bundle.ErrProjectNotFound
			},
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/projects/nope/planning/context-bundle")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMountBundleRouteWithAdapter(t *testing.T) {
	root := setupRouteProject(t)
	r := chi.NewRouter()
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		_ = MountBundleRoute(r, BundleRouteDeps{
			Orchestrator: bundle.NewAssemblyOrchestrator(),
			Resolver: func(ctx context.Context, id string) (bundle.BuildOpts, error) {
				return bundle.BuildOpts{
					ProjectID:   id,
					ProjectRoot: root,
					CommitSHA:   strings.Repeat("a", 40),
					TokenBudget: 100_000,
				}, nil
			},
			Adapter: func(ctx context.Context, id string) (bundle.AssemblyInput, error) {
				return bundle.AssemblyInput{
					Beads: []bundle.RawBead{{Id: "hp-x", IssueType: "task"}},
				}, nil
			},
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/projects/demo/planning/context-bundle")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMountBundleRouteAdapterErrorReturns500(t *testing.T) {
	root := setupRouteProject(t)
	r := chi.NewRouter()
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		_ = MountBundleRoute(r, BundleRouteDeps{
			Orchestrator: bundle.NewAssemblyOrchestrator(),
			Resolver: func(ctx context.Context, id string) (bundle.BuildOpts, error) {
				return bundle.BuildOpts{
					ProjectID:   id,
					ProjectRoot: root,
					CommitSHA:   strings.Repeat("a", 40),
					TokenBudget: 100_000,
				}, nil
			},
			Adapter: func(ctx context.Context, id string) (bundle.AssemblyInput, error) {
				return bundle.AssemblyInput{}, errors.New("br offline")
			},
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/projects/demo/planning/context-bundle")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestMountBundleRouteWithCacheParameterAccepted(t *testing.T) {
	// Verify the Cache field doesn't crash construction even when
	// the cached-handler path is still pending the future slice.
	root := setupRouteProject(t)
	r := chi.NewRouter()
	r.Route("/v1/projects/{projectId}", func(r chi.Router) {
		err := MountBundleRoute(r, BundleRouteDeps{
			Orchestrator: bundle.NewAssemblyOrchestrator(),
			Cache:        bundle.NewBundleCache(),
			Resolver: func(ctx context.Context, id string) (bundle.BuildOpts, error) {
				return bundle.BuildOpts{
					ProjectID:   id,
					ProjectRoot: root,
					CommitSHA:   strings.Repeat("a", 40),
					TokenBudget: 100_000,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("MountBundleRoute with Cache: %v", err)
		}
	})

	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/projects/x/planning/context-bundle")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
