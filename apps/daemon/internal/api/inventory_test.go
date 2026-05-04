package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/inventory"
)

func TestInventoryRoutesUseConfiguredService(t *testing.T) {
	service := &fakeInventoryService{
		snapshot: &inventory.Snapshot{
			SchemaVersion: inventory.SchemaVersion,
			SnapshotAt:    "2026-05-04T04:00:00Z",
			Tools: []inventory.Tool{{
				Name: "br",
			}},
		},
		refresh: &inventory.Snapshot{
			SchemaVersion: inventory.SchemaVersion,
			SnapshotAt:    "2026-05-04T04:01:00Z",
			Tools: []inventory.Tool{{
				Name: "git",
			}},
		},
	}
	router := NewRouter(Config{Inventory: service})

	req := httptest.NewRequest(http.MethodGet, "/v1/inventory/tools", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got inventory.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if got.SnapshotAt != "2026-05-04T04:00:00Z" || got.Tools[0].Name != "br" {
		t.Fatalf("GET snapshot = %+v", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/inventory/tools/refresh", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode POST response: %v", err)
	}
	if got.SnapshotAt != "2026-05-04T04:01:00Z" || got.Tools[0].Name != "git" {
		t.Fatalf("POST snapshot = %+v", got)
	}
	if service.snapshotCalls != 1 || service.refreshCalls != 1 {
		t.Fatalf("service calls snapshot=%d refresh=%d", service.snapshotCalls, service.refreshCalls)
	}
}

func TestInventoryRouteWithoutServiceReturnsProblem(t *testing.T) {
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/inventory/tools", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
}

func TestInventoryRefreshTimeoutReturnsProblem(t *testing.T) {
	router := NewRouter(Config{Inventory: &fakeInventoryService{refreshErr: context.DeadlineExceeded}})
	req := httptest.NewRequest(http.MethodPost, "/v1/inventory/tools/refresh", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestTimeout)
	}
}

type fakeInventoryService struct {
	snapshot *inventory.Snapshot
	refresh  *inventory.Snapshot

	snapshotErr error
	refreshErr  error

	snapshotCalls int
	refreshCalls  int
}

func (f *fakeInventoryService) Snapshot(context.Context) (*inventory.Snapshot, error) {
	f.snapshotCalls++
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	if f.snapshot == nil {
		return nil, errors.New("missing snapshot")
	}
	return f.snapshot, nil
}

func (f *fakeInventoryService) Refresh(context.Context) (*inventory.Snapshot, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	if f.refresh == nil {
		return nil, errors.New("missing refresh")
	}
	return f.refresh, nil
}
