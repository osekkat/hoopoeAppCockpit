package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	providerplugins "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/providers"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func (s *server) mountProviderRoutes(r chi.Router) {
	r.Get("/v1/providers", s.handleProviderList)
	r.Route("/v1/providers/{providerId}", func(r chi.Router) {
		r.Get("/regions", s.handleProviderRegions)
		r.Get("/regions/{regionId}/sizes", s.handleProviderSizes)
		r.Post("/cost-estimate", s.handleProviderCostEstimate)
		r.Post("/instances", s.handleProviderCreateInstance)
		r.Delete("/instances/{instanceId}", s.handleProviderDestroyInstance)
	})
}

func (s *server) handleProviderList(w http.ResponseWriter, _ *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusOK, schemas.ProviderListResponse{Items: []schemas.ProviderPluginManifest{}})
		return
	}
	plugins := s.providers.List()
	items := make([]schemas.ProviderPluginManifest, 0, len(plugins))
	for _, plugin := range plugins {
		items = append(items, plugin.Manifest())
	}
	writeJSON(w, http.StatusOK, schemas.ProviderListResponse{Items: items})
}

func (s *server) handleProviderRegions(w http.ResponseWriter, r *http.Request) {
	id, plugin, ok := s.providerPlugin(w, r, schemas.VpsListRegions)
	if !ok {
		return
	}
	items, err := plugin.ListRegions(r.Context())
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ProviderRegionListResponse{ProviderId: id, Items: items})
}

func (s *server) handleProviderSizes(w http.ResponseWriter, r *http.Request) {
	id, plugin, ok := s.providerPlugin(w, r, schemas.VpsListSizes)
	if !ok {
		return
	}
	regionID := strings.TrimSpace(chi.URLParam(r, "regionId"))
	if regionID == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "provider.invalid_region", "invalid provider region", "regionId is required")
		return
	}
	items, err := plugin.ListSizes(r.Context(), regionID)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ProviderSizeListResponse{ProviderId: id, RegionId: regionID, Items: items})
}

func (s *server) handleProviderCostEstimate(w http.ResponseWriter, r *http.Request) {
	_, plugin, ok := s.providerPlugin(w, r, schemas.VpsEstimateCost)
	if !ok {
		return
	}
	var request schemas.ProviderEstimateCostOpts
	if err := decodeRequiredJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	estimate, err := plugin.EstimateMonthlyCost(r.Context(), request)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, estimate)
}

func (s *server) handleProviderCreateInstance(w http.ResponseWriter, r *http.Request) {
	_, plugin, ok := s.providerPlugin(w, r, schemas.VpsCreate)
	if !ok {
		return
	}
	var request schemas.ProviderCreateInstanceOpts
	if err := decodeRequiredJSON(r, &request); err != nil {
		s.writeProblemCode(w, http.StatusBadRequest, "request.invalid_json", "invalid request body", err.Error())
		return
	}
	instance, err := plugin.CreateInstance(r.Context(), request)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, instance)
}

func (s *server) handleProviderDestroyInstance(w http.ResponseWriter, r *http.Request) {
	_, plugin, ok := s.providerPlugin(w, r, schemas.VpsDestroy)
	if !ok {
		return
	}
	instanceID := strings.TrimSpace(chi.URLParam(r, "instanceId"))
	if instanceID == "" {
		s.writeProblemCode(w, http.StatusBadRequest, "provider.invalid_instance", "invalid provider instance", "instanceId is required")
		return
	}
	result, err := plugin.DestroyInstance(r.Context(), instanceID)
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) providerPlugin(w http.ResponseWriter, r *http.Request, capability schemas.ProviderPluginManifestCapabilities) (schemas.ProviderId, schemas.ProviderPlugin, bool) {
	id, ok := s.providerIDParam(w, r)
	if !ok {
		return "", nil, false
	}
	if s.providers == nil {
		s.writeProviderError(w, fmt.Errorf("%w: %s", schemas.ErrProviderNotFound, id))
		return "", nil, false
	}
	plugin, err := s.providerByRegisteredManifestID(id)
	if err != nil {
		s.writeProviderError(w, err)
		return "", nil, false
	}
	if err := providerplugins.RequireCapability(plugin, capability); err != nil {
		s.writeProviderError(w, err)
		return "", nil, false
	}
	return id, plugin, true
}

func (s *server) providerByRegisteredManifestID(id schemas.ProviderId) (schemas.ProviderPlugin, error) {
	for _, plugin := range s.providers.List() {
		if plugin.Manifest().ProviderId == id {
			return plugin, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", schemas.ErrProviderNotFound, id)
}

func (s *server) providerIDParam(w http.ResponseWriter, r *http.Request) (schemas.ProviderId, bool) {
	id := schemas.ProviderId(strings.TrimSpace(chi.URLParam(r, "providerId")))
	if !validProviderID(id) {
		s.writeProblemCode(w, http.StatusBadRequest, "provider.invalid_id", "invalid provider id", "providerId must be 1-128 characters of letters, numbers, dot, dash, or underscore")
		return "", false
	}
	return id, true
}

func validProviderID(id schemas.ProviderId) bool {
	value := string(id)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *server) writeProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, schemas.ErrProviderNotFound):
		s.writeProblemCode(w, http.StatusNotFound, "provider.not_found", "provider plugin not found", err.Error())
	case errors.Is(err, schemas.ErrProviderMethodUnsupported):
		s.writeProblemCode(w, http.StatusNotImplemented, "provider.method_unsupported", "provider method unsupported", err.Error())
	case errors.Is(err, providerplugins.ErrInvalidPlugin):
		s.writeProblemCode(w, http.StatusInternalServerError, "provider.invalid_plugin", "invalid provider plugin", err.Error())
	default:
		s.writeProblemCode(w, http.StatusBadGateway, "provider.request_failed", "provider request failed", err.Error())
	}
}
