package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupHandlerProject(t *testing.T) string {
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
	must("README.md", []byte("# Test"))
	must("package.json", []byte(`{"name":"x"}`))
	return root
}

func TestNewHandlerNilOrchestratorRejected(t *testing.T) {
	_, err := NewHandler(HandlerConfig{
		Resolver: func(context.Context, string) (BuildOpts, error) { return BuildOpts{}, nil },
	})
	if err == nil {
		t.Fatal("nil orchestrator should error at construction")
	}
}

func TestNewHandlerNilResolverRejected(t *testing.T) {
	_, err := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
	})
	if err == nil {
		t.Fatal("nil resolver should error at construction")
	}
}

func TestHandlerSuccessfulBuild(t *testing.T) {
	root := setupHandlerProject(t)
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{
			ProjectID:   id,
			ProjectRoot: root,
			CommitSHA:   strings.Repeat("a", 40),
			TokenBudget: 100_000,
		}, nil
	}
	h, err := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/demo/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "demo")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if hash := w.Header().Get("X-Bundle-Content-Hash"); hash == "" {
		t.Error("X-Bundle-Content-Hash header not set")
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	if body["projectId"] != "demo" {
		t.Errorf("projectId in body = %v, want demo", body["projectId"])
	}
	if body["contentHash"] == "" {
		t.Errorf("contentHash empty")
	}
}

func TestHandlerProjectNotFoundReturns404(t *testing.T) {
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{}, ErrProjectNotFound
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/nope/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "nope")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "planning.bundle.project_not_found" {
		t.Errorf("code = %v, want planning.bundle.project_not_found", body["code"])
	}
}

func TestHandlerResolverErrorReturns500(t *testing.T) {
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{}, errors.New("db connection lost")
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/x/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestHandlerEmptyProjectIDReturns400(t *testing.T) {
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     func(context.Context, string) (BuildOpts, error) { return BuildOpts{}, nil },
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects//planning/context-bundle", nil)
	// No X-Hoopoe-Project-Id header → empty project ID.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandlerInvalidBuildOptsReturns400(t *testing.T) {
	root := setupHandlerProject(t)
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{
			ProjectID:   id,
			ProjectRoot: root,
			CommitSHA:   "too-short", // fails validate()
			TokenBudget: 100_000,
		}, nil
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/demo/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "demo")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "planning.bundle.invalid_opts" {
		t.Errorf("code = %v, want planning.bundle.invalid_opts", body["code"])
	}
}

func TestHandlerAdapterErrorReturns500(t *testing.T) {
	root := setupHandlerProject(t)
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{
			ProjectID:   id,
			ProjectRoot: root,
			CommitSHA:   strings.Repeat("a", 40),
			TokenBudget: 100_000,
		}, nil
	}
	adapter := func(ctx context.Context, id string) (AssemblyInput, error) {
		return AssemblyInput{}, errors.New("br adapter offline")
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
		Adapter:      adapter,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/x/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "planning.bundle.adapter_error" {
		t.Errorf("code = %v, want planning.bundle.adapter_error", body["code"])
	}
}

func TestHandlerWithAdapterPipesBeadsAndHotspots(t *testing.T) {
	root := setupHandlerProject(t)
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{
			ProjectID:   id,
			ProjectRoot: root,
			CommitSHA:   strings.Repeat("a", 40),
			TokenBudget: 100_000,
		}, nil
	}
	adapter := func(ctx context.Context, id string) (AssemblyInput, error) {
		return AssemblyInput{
			Beads:    []RawBead{{Id: "hp-x", IssueType: "task"}},
			Hotspots: []RawHotspot{{Path: "src/y.ts", CompositeScore: 50}},
		}, nil
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
		Adapter:      adapter,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/demo/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "demo")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"hp-x"`) {
		t.Errorf("response missing bead hp-x: %s", body)
	}
	if !strings.Contains(body, `"src/y.ts"`) {
		t.Errorf("response missing hotspot src/y.ts: %s", body)
	}
}

func TestHandlerProblemEnvelopeContentType(t *testing.T) {
	resolver := func(ctx context.Context, id string) (BuildOpts, error) {
		return BuildOpts{}, ErrProjectNotFound
	}
	h, _ := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		Resolver:     resolver,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/x/planning/context-bundle", nil)
	req.Header.Set("X-Hoopoe-Project-Id", "x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
}

func TestHandlerCustomPathParam(t *testing.T) {
	// Custom PathParam is supported but our default extractor reads
	// from a fixed header — verify the construction path doesn't
	// fail and produces a working handler.
	root := setupHandlerProject(t)
	h, err := NewHandler(HandlerConfig{
		Orchestrator: NewAssemblyOrchestrator(),
		PathParam:    "projId",
		Resolver: func(ctx context.Context, id string) (BuildOpts, error) {
			return BuildOpts{
				ProjectID:   id,
				ProjectRoot: root,
				CommitSHA:   strings.Repeat("a", 40),
				TokenBudget: 100_000,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if h == nil {
		t.Fatal("handler nil despite no error")
	}
}
