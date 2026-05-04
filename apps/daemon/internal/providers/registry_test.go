package providers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/providers"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/providers/contabo"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestRegistryRegistersListsAndGetsPlugins(t *testing.T) {
	t.Parallel()
	first := testPlugin("contabo", schemas.VpsListRegions, schemas.VpsEstimateCost)
	second := testPlugin("ovh", schemas.VpsListRegions)

	registry, err := providers.NewRegistry(first, second)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Get("contabo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != first {
		t.Fatalf("got plugin %p, want %p", got, first)
	}
	listed := registry.List()
	if len(listed) != 2 || listed[0] != first || listed[1] != second {
		t.Fatalf("listed = %#v", listed)
	}
	listed[0] = second
	listedAgain := registry.List()
	if listedAgain[0] != first {
		t.Fatalf("List returned mutable internal slice")
	}
}

func TestRegistryRejectsDuplicateOrMissingPlugins(t *testing.T) {
	t.Parallel()
	registry, err := providers.NewRegistry(testPlugin("contabo", schemas.VpsListRegions))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := registry.Register(testPlugin("contabo", schemas.VpsListSizes)); !errors.Is(err, schemas.ErrProviderAlreadyRegistered) {
		t.Fatalf("duplicate err = %v, want ErrProviderAlreadyRegistered", err)
	}
	if _, err := registry.Get("missing"); !errors.Is(err, schemas.ErrProviderNotFound) {
		t.Fatalf("missing err = %v, want ErrProviderNotFound", err)
	}
}

func TestRegistryValidatesPluginManifest(t *testing.T) {
	t.Parallel()
	tests := map[string]schemas.ProviderPluginManifest{
		"missing id": {
			SchemaVersion: 1,
			DisplayName:   "Broken",
			AuthMode:      schemas.ApiToken,
			Capabilities:  []schemas.ProviderPluginManifestCapabilities{schemas.VpsListRegions},
		},
		"bad auth": {
			SchemaVersion: 1,
			ProviderId:    "broken",
			DisplayName:   "Broken",
			AuthMode:      "keychain",
			Capabilities:  []schemas.ProviderPluginManifestCapabilities{schemas.VpsListRegions},
		},
		"bad capability": {
			SchemaVersion: 1,
			ProviderId:    "broken",
			DisplayName:   "Broken",
			AuthMode:      schemas.ApiToken,
			Capabilities:  []schemas.ProviderPluginManifestCapabilities{"vps.teleport"},
		},
		"duplicate capability": {
			SchemaVersion: 1,
			ProviderId:    "broken",
			DisplayName:   "Broken",
			AuthMode:      schemas.ApiToken,
			Capabilities:  []schemas.ProviderPluginManifestCapabilities{schemas.VpsListRegions, schemas.VpsListRegions},
		},
	}
	for name, manifest := range tests {
		name := name
		manifest := manifest
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := providers.NewRegistry(staticManifestPlugin{manifest: manifest})
			if !errors.Is(err, providers.ErrInvalidPlugin) {
				t.Fatalf("err = %v, want ErrInvalidPlugin", err)
			}
		})
	}
}

func TestRequireCapability(t *testing.T) {
	t.Parallel()
	plugin := testPlugin("contabo", schemas.VpsListRegions)
	if err := providers.RequireCapability(plugin, schemas.VpsListRegions); err != nil {
		t.Fatalf("RequireCapability supported: %v", err)
	}
	if err := providers.RequireCapability(plugin, schemas.VpsDestroy); !errors.Is(err, schemas.ErrProviderMethodUnsupported) {
		t.Fatalf("unsupported err = %v, want ErrProviderMethodUnsupported", err)
	}
}

func TestContaboPluginRegistersInProviderRegistry(t *testing.T) {
	t.Parallel()
	registry, err := providers.NewRegistry(contabo.New(contabo.Options{}))
	if err != nil {
		t.Fatalf("NewRegistry(contabo): %v", err)
	}
	plugin, err := registry.Get(contabo.ProviderID)
	if err != nil {
		t.Fatalf("Get(contabo): %v", err)
	}
	manifest := plugin.Manifest()
	if manifest.ProviderId != contabo.ProviderID || !manifest.HasCapability(schemas.VpsCreate) {
		t.Fatalf("manifest = %+v", manifest)
	}
}

type staticManifestPlugin struct {
	manifest schemas.ProviderPluginManifest
}

func testPlugin(id schemas.ProviderId, capabilities ...schemas.ProviderPluginManifestCapabilities) *staticManifestPlugin {
	return &staticManifestPlugin{
		manifest: schemas.ProviderPluginManifest{
			SchemaVersion: 1,
			ProviderId:    id,
			DisplayName:   string(id) + " provider",
			AuthMode:      schemas.ApiToken,
			Capabilities:  capabilities,
		},
	}
}

func (p staticManifestPlugin) Manifest() schemas.ProviderPluginManifest {
	return p.manifest
}

func (p staticManifestPlugin) ListRegions(context.Context) ([]schemas.ProviderRegion, error) {
	return []schemas.ProviderRegion{{Id: "eu-central-1", Name: "EU Central", Country: "DE", Available: true}}, nil
}

func (p staticManifestPlugin) ListSizes(context.Context, string) ([]schemas.ProviderSize, error) {
	return []schemas.ProviderSize{{Id: "small", CpuVCores: 8, RamGB: 32, StorageGB: 100, StorageType: schemas.NVMe, BandwidthTB: 10, MonthlyUSD: 40, Tier: schemas.Minimum}}, nil
}

func (p staticManifestPlugin) EstimateMonthlyCost(context.Context, schemas.ProviderEstimateCostOpts) (*schemas.ProviderCostEstimate, error) {
	return &schemas.ProviderCostEstimate{
		Usd:            40,
		CatalogVersion: "test",
		EstimatedAt:    time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (p staticManifestPlugin) CreateInstance(context.Context, schemas.ProviderCreateInstanceOpts) (*schemas.ProviderInstance, error) {
	return &schemas.ProviderInstance{InstanceId: "i-test", Region: "eu-central-1", Size: "small", Status: schemas.Running}, nil
}

func (p staticManifestPlugin) DestroyInstance(_ context.Context, instanceID string) (*schemas.ProviderDestroyResult, error) {
	return &schemas.ProviderDestroyResult{Ok: true, InstanceId: instanceID}, nil
}
