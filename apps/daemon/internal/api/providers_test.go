package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	providerplugins "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/providers"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestProviderRoutesListEmptyWhenRegistryUnconfigured(t *testing.T) {
	t.Parallel()
	router := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/providers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body schemas.ProviderListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode provider list: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("items = %+v, want empty", body.Items)
	}
}

func TestProviderReadRoutesUseRegistry(t *testing.T) {
	t.Parallel()
	plugin := newRecordingProvider()
	router := newProviderRouter(t, plugin)

	t.Run("list providers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/providers", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body schemas.ProviderListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode provider list: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].ProviderId != "contabo" {
			t.Fatalf("provider list = %+v", body.Items)
		}
	})

	t.Run("regions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/contabo/regions", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body schemas.ProviderRegionListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode regions: %v", err)
		}
		if body.ProviderId != "contabo" || len(body.Items) != 1 || body.Items[0].Id != "eu-central-1" {
			t.Fatalf("regions = %+v", body)
		}
	})

	t.Run("sizes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/providers/contabo/regions/eu-central-1/sizes", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body schemas.ProviderSizeListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode sizes: %v", err)
		}
		if body.ProviderId != "contabo" || body.RegionId != "eu-central-1" || len(body.Items) != 1 || body.Items[0].Id != "cloud-vps-50" {
			t.Fatalf("sizes = %+v", body)
		}
		if plugin.lastRegion != "eu-central-1" {
			t.Fatalf("lastRegion = %q, want eu-central-1", plugin.lastRegion)
		}
	})
}

func TestProviderWriteRoutesUseRegistry(t *testing.T) {
	t.Parallel()
	plugin := newRecordingProvider()
	router := newProviderRouter(t, plugin)

	t.Run("cost estimate", func(t *testing.T) {
		payload := []byte(`{"region":"us-east-1","size":"cloud-vps-50","bandwidthTBExpected":33}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/providers/contabo/cost-estimate", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body schemas.ProviderCostEstimate
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode estimate: %v", err)
		}
		if body.Usd != 66 || plugin.lastEstimate.Region != "us-east-1" || plugin.lastEstimate.Size != "cloud-vps-50" {
			t.Fatalf("estimate = %+v last=%+v", body, plugin.lastEstimate)
		}
	})

	t.Run("create instance", func(t *testing.T) {
		payload := []byte(`{"region":"eu-central-1","size":"cloud-vps-50","name":"hoopoe-test","sshPubKey":"ssh-ed25519 AAAATEST"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/providers/contabo/instances", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
		}
		var body schemas.ProviderInstance
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode instance: %v", err)
		}
		if body.InstanceId != "i-123" || plugin.lastCreate.Name != "hoopoe-test" {
			t.Fatalf("instance = %+v lastCreate=%+v", body, plugin.lastCreate)
		}
	})

	t.Run("destroy instance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v1/providers/contabo/instances/i-123", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var body schemas.ProviderDestroyResult
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode destroy: %v", err)
		}
		if !body.Ok || body.InstanceId != "i-123" || plugin.lastDestroy != "i-123" {
			t.Fatalf("destroy = %+v lastDestroy=%q", body, plugin.lastDestroy)
		}
	})
}

func TestProviderRoutesReturnProblemForMissingOrUnsupportedProvider(t *testing.T) {
	t.Parallel()
	plugin := newRecordingProvider()
	plugin.manifest.Capabilities = []schemas.ProviderPluginManifestCapabilities{schemas.VpsListRegions}
	router := newProviderRouter(t, plugin)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{name: "missing", method: http.MethodGet, path: "/v1/providers/missing/regions", status: http.StatusNotFound, code: "provider.not_found"},
		{name: "unsupported", method: http.MethodPost, path: "/v1/providers/contabo/cost-estimate", status: http.StatusNotImplemented, code: "provider.method_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(`{"region":"eu-central-1","size":"cloud-vps-50"}`)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			var body schemas.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if body.Code != tc.code {
				t.Fatalf("problem = %+v, want code %s", body, tc.code)
			}
		})
	}
}

func TestProviderRoutesRejectInvalidJSON(t *testing.T) {
	t.Parallel()
	router := newProviderRouter(t, newRecordingProvider())
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/contabo/cost-estimate", bytes.NewReader([]byte(`{"region":"eu-central-1","size":"cloud-vps-50","extra":true}`)))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body schemas.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != "request.invalid_json" {
		t.Fatalf("problem = %+v", body)
	}
}

func newProviderRouter(t *testing.T, plugin schemas.ProviderPlugin) http.Handler {
	t.Helper()
	registry, err := providerplugins.NewRegistry(plugin)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return NewRouter(Config{Providers: registry})
}

type recordingProvider struct {
	manifest     schemas.ProviderPluginManifest
	lastRegion   string
	lastEstimate schemas.ProviderEstimateCostOpts
	lastCreate   schemas.ProviderCreateInstanceOpts
	lastDestroy  string
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{
		manifest: schemas.ProviderPluginManifest{
			SchemaVersion: 1,
			ProviderId:    "contabo",
			DisplayName:   "Contabo",
			AuthMode:      schemas.ApiToken,
			Capabilities: []schemas.ProviderPluginManifestCapabilities{
				schemas.VpsListRegions,
				schemas.VpsListSizes,
				schemas.VpsEstimateCost,
				schemas.VpsCreate,
				schemas.VpsDestroy,
			},
		},
	}
}

func (p *recordingProvider) Manifest() schemas.ProviderPluginManifest {
	return p.manifest
}

func (p *recordingProvider) ListRegions(context.Context) ([]schemas.ProviderRegion, error) {
	return []schemas.ProviderRegion{{Id: "eu-central-1", Name: "EU Central", Country: "DE", Available: true}}, nil
}

func (p *recordingProvider) ListSizes(_ context.Context, regionID string) ([]schemas.ProviderSize, error) {
	if regionID == "" {
		return nil, errors.New("region required")
	}
	p.lastRegion = regionID
	return []schemas.ProviderSize{{
		Id:          "cloud-vps-50",
		CpuVCores:   16,
		RamGB:       64,
		StorageGB:   300,
		StorageType: schemas.NVMe,
		BandwidthTB: 32,
		MonthlyUSD:  56,
		Tier:        schemas.Recommended,
	}}, nil
}

func (p *recordingProvider) EstimateMonthlyCost(_ context.Context, opts schemas.ProviderEstimateCostOpts) (*schemas.ProviderCostEstimate, error) {
	p.lastEstimate = opts
	return &schemas.ProviderCostEstimate{
		Usd:            66,
		CatalogVersion: "test",
		EstimatedAt:    time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		Breakdown: []schemas.ProviderCostLineItem{
			{Label: "compute", Usd: 56},
			{Label: "datacenter-surcharge", Usd: 10},
		},
	}, nil
}

func (p *recordingProvider) CreateInstance(_ context.Context, opts schemas.ProviderCreateInstanceOpts) (*schemas.ProviderInstance, error) {
	p.lastCreate = opts
	return &schemas.ProviderInstance{
		InstanceId: "i-123",
		Region:     opts.Region,
		Size:       opts.Size,
		Status:     schemas.Provisioning,
		CreatedAt:  time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		MonthlyUSD: 56,
	}, nil
}

func (p *recordingProvider) DestroyInstance(_ context.Context, instanceID string) (*schemas.ProviderDestroyResult, error) {
	p.lastDestroy = instanceID
	return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID}, nil
}
