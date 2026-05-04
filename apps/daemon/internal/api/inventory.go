package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/caam"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/inventory"
)

type InventoryService interface {
	Snapshot(ctx context.Context) (*inventory.Snapshot, error)
	Refresh(ctx context.Context) (*inventory.Snapshot, error)
}

func resolveInventoryService(configured InventoryService, registry *capabilities.Registry, now func() time.Time) InventoryService {
	if configured != nil {
		return configured
	}
	if registry == nil {
		return nil
	}
	return inventory.NewService(inventory.Config{
		Registry: registry,
		CAAM:     caam.New(),
		Now:      now,
	})
}

func (s *server) mountInventoryRoutes(r chi.Router) {
	r.Get("/v1/inventory/tools", s.handleInventorySnapshot)
	r.Post("/v1/inventory/tools/refresh", s.handleInventoryRefresh)
}

func (s *server) handleInventorySnapshot(w http.ResponseWriter, r *http.Request) {
	if s.inventory == nil {
		s.writeProblemCode(
			w,
			http.StatusServiceUnavailable,
			"inventory.not_configured",
			"tool inventory not wired",
			"the daemon was started without an inventory service or capability registry",
		)
		return
	}
	snapshot, err := s.inventory.Snapshot(r.Context())
	if err != nil {
		s.writeInventoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) handleInventoryRefresh(w http.ResponseWriter, r *http.Request) {
	if s.inventory == nil {
		s.writeProblemCode(
			w,
			http.StatusServiceUnavailable,
			"inventory.not_configured",
			"tool inventory not wired",
			"the daemon was started without an inventory service or capability registry",
		)
		return
	}
	snapshot, err := s.inventory.Refresh(r.Context())
	if err != nil {
		s.writeInventoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) writeInventoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, inventory.ErrRegistryUnavailable):
		s.writeProblemCode(
			w,
			http.StatusServiceUnavailable,
			"inventory.registry_unavailable",
			"capability registry unavailable",
			err.Error(),
		)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		s.writeProblemCode(
			w,
			http.StatusRequestTimeout,
			"inventory.refresh_timeout",
			"tool inventory refresh timed out",
			err.Error(),
		)
	default:
		s.writeProblemCode(
			w,
			http.StatusInternalServerError,
			"inventory.snapshot_failed",
			"tool inventory failed",
			err.Error(),
		)
	}
}
