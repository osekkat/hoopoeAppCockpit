// provider_test.go — exercises the ProviderPlugin interface contract via a
// mock implementation.
//
// What this proves:
//   1. The interface compiles (build-time check).
//   2. A mock plugin satisfying every method works end-to-end through
//      Register → Get → List → contract-method calls.
//   3. Manifest.HasCapability matches enum values from the generated
//      ProviderPluginManifestCapabilities set.
//   4. The cost-estimate breakdown sum equals the total (a contract
//      invariant the renderer also asserts).
//   5. DestroyInstance is idempotent (second call returns ok:true,
//      not an error).
package schemas_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// mockProvider is a minimal Contabo-shaped plugin that implements every
// method on schemas.ProviderPlugin. Lives in this test file (not the
// production package) so plugin-package authors can read it as the
// canonical "how to implement the contract" example.
type mockProvider struct {
	manifest schemas.ProviderPluginManifest
	regions  []schemas.ProviderRegion
	sizes    map[string][]schemas.ProviderSize
	mu       sync.Mutex
	created  map[string]*schemas.ProviderInstance
	destroyed map[string]bool
	now      func() time.Time
}

func newMockContabo() *mockProvider {
	return &mockProvider{
		manifest: schemas.ProviderPluginManifest{
			SchemaVersion: 1,
			ProviderId:    "contabo",
			DisplayName:   "Contabo Cloud VPS",
			AuthMode:      schemas.ApiToken,
			Capabilities: []schemas.ProviderPluginManifestCapabilities{
				"vps.list-regions",
				"vps.list-sizes",
				"vps.create",
				"vps.destroy",
				"vps.estimate-cost",
			},
		},
		regions: []schemas.ProviderRegion{
			{Id: "eu-central-1", Name: "Frankfurt, Germany", Country: "DE", Available: true},
			{Id: "us-east-1", Name: "Virginia, USA", Country: "US", Available: true},
		},
		sizes: map[string][]schemas.ProviderSize{
			"eu-central-1": {
				{
					Id: "cloud-vps-50", CpuVCores: 8, RamGB: 64, StorageGB: 1500,
					StorageType: "NVMe", BandwidthTB: 32, MonthlyUSD: 56,
					Tier: "recommended",
				},
				{
					Id: "cloud-vps-30", CpuVCores: 6, RamGB: 32, StorageGB: 800,
					StorageType: "NVMe", BandwidthTB: 32, MonthlyUSD: 40,
					Tier: "workable",
				},
			},
		},
		created:   map[string]*schemas.ProviderInstance{},
		destroyed: map[string]bool{},
		now:       time.Now,
	}
}

func (m *mockProvider) Manifest() schemas.ProviderPluginManifest { return m.manifest }

func (m *mockProvider) ListRegions(ctx context.Context) ([]schemas.ProviderRegion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]schemas.ProviderRegion{}, m.regions...), nil
}

func (m *mockProvider) ListSizes(ctx context.Context, regionID string) ([]schemas.ProviderSize, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sizes, ok := m.sizes[regionID]
	if !ok {
		return nil, errors.New("unknown region")
	}
	return append([]schemas.ProviderSize{}, sizes...), nil
}

func (m *mockProvider) EstimateMonthlyCost(ctx context.Context, opts schemas.ProviderEstimateCostOpts) (*schemas.ProviderCostEstimate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sizes := m.sizes[opts.Region]
	for _, s := range sizes {
		if s.Id == opts.Size {
			return &schemas.ProviderCostEstimate{
				Usd:      s.MonthlyUSD,
				Currency: ptr("EUR"),
				Breakdown: []schemas.ProviderCostLineItem{
					{Label: "compute", Usd: s.MonthlyUSD - 6 - 2},
					{Label: "bandwidth", Usd: 6},
					{Label: "storage", Usd: 2},
				},
				CatalogVersion: "contabo-2026-05-04T00:00",
				EstimatedAt:    m.now().UTC(),
			}, nil
		}
	}
	return nil, errors.New("unknown size")
}

func (m *mockProvider) CreateInstance(ctx context.Context, opts schemas.ProviderCreateInstanceOpts) (*schemas.ProviderInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "inst_" + opts.Name
	if existing, ok := m.created[id]; ok {
		// Idempotent retry: same name → same instance (not a fresh one).
		return existing, nil
	}
	monthly := float32(56.0)
	for _, s := range m.sizes[opts.Region] {
		if s.Id == opts.Size {
			monthly = s.MonthlyUSD
		}
	}
	inst := &schemas.ProviderInstance{
		InstanceId: id,
		Status:     "provisioning",
		CreatedAt:  m.now().UTC(),
		Region:     opts.Region,
		Size:       opts.Size,
		MonthlyUSD: monthly,
	}
	m.created[id] = inst
	return inst, nil
}

func (m *mockProvider) DestroyInstance(ctx context.Context, instanceID string) (*schemas.ProviderDestroyResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.created, instanceID)
	m.destroyed[instanceID] = true
	notes := "instance destroyed"
	return &schemas.ProviderDestroyResult{
		Ok:         true,
		InstanceId: instanceID,
		Notes:      &notes,
	}, nil
}

// inMemoryRegistry is a minimal ProviderRegistry implementation used by
// these tests. The daemon will ship its own (with init() registration),
// but this proves the interface is implementable.
type inMemoryRegistry struct {
	mu      sync.Mutex
	plugins []schemas.ProviderPlugin
	byID    map[schemas.ProviderId]schemas.ProviderPlugin
}

func newRegistry() *inMemoryRegistry {
	return &inMemoryRegistry{byID: map[schemas.ProviderId]schemas.ProviderPlugin{}}
}

func (r *inMemoryRegistry) Register(p schemas.ProviderPlugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := schemas.ProviderId(p.Manifest().ProviderId)
	if _, ok := r.byID[id]; ok {
		return schemas.ErrProviderAlreadyRegistered
	}
	r.byID[id] = p
	r.plugins = append(r.plugins, p)
	return nil
}

func (r *inMemoryRegistry) Get(id schemas.ProviderId) (schemas.ProviderPlugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return nil, schemas.ErrProviderNotFound
	}
	return p, nil
}

func (r *inMemoryRegistry) List() []schemas.ProviderPlugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schemas.ProviderPlugin{}, r.plugins...)
}

func TestProviderPluginInterfaceSatisfiesContract(t *testing.T) {
	t.Parallel()

	var _ schemas.ProviderPlugin = (*mockProvider)(nil)
	var _ schemas.ProviderRegistry = (*inMemoryRegistry)(nil)

	ctx := context.Background()
	plugin := newMockContabo()

	regs, err := plugin.ListRegions(ctx)
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(regs) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(regs))
	}
	if regs[0].Country != "DE" {
		t.Fatalf("expected DE country, got %q", regs[0].Country)
	}

	sizes, err := plugin.ListSizes(ctx, "eu-central-1")
	if err != nil {
		t.Fatalf("ListSizes: %v", err)
	}
	if len(sizes) != 2 {
		t.Fatalf("expected 2 sizes, got %d", len(sizes))
	}
	if sizes[0].Tier != "recommended" {
		t.Fatalf("expected recommended tier, got %q", sizes[0].Tier)
	}

	estimate, err := plugin.EstimateMonthlyCost(ctx, schemas.ProviderEstimateCostOpts{
		Region: "eu-central-1",
		Size:   "cloud-vps-50",
	})
	if err != nil {
		t.Fatalf("EstimateMonthlyCost: %v", err)
	}
	if estimate.Usd != 56 {
		t.Fatalf("expected $56 estimate, got $%v", estimate.Usd)
	}
	// Contract invariant: breakdown sums to total.
	var sum float32
	for _, item := range estimate.Breakdown {
		sum += item.Usd
	}
	if sum != estimate.Usd {
		t.Fatalf("breakdown sum %v != total %v", sum, estimate.Usd)
	}
}

func TestProviderPluginCreateThenDestroyIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	plugin := newMockContabo()
	opts := schemas.ProviderCreateInstanceOpts{
		Region:    "eu-central-1",
		Size:      "cloud-vps-50",
		SshPubKey: "ssh-ed25519 AAAA... user@host",
		Name:      "hoopoe-acfs-test",
		ImageId:   "ubuntu-24.04",
	}

	first, err := plugin.CreateInstance(ctx, opts)
	if err != nil {
		t.Fatalf("CreateInstance #1: %v", err)
	}
	second, err := plugin.CreateInstance(ctx, opts)
	if err != nil {
		t.Fatalf("CreateInstance #2 (idempotent retry): %v", err)
	}
	if first.InstanceId != second.InstanceId {
		t.Fatalf("idempotent retry returned different ID: %q vs %q",
			first.InstanceId, second.InstanceId)
	}

	// Destroy then destroy again — second call must return ok:true (not error).
	r1, err := plugin.DestroyInstance(ctx, first.InstanceId)
	if err != nil {
		t.Fatalf("DestroyInstance #1: %v", err)
	}
	if !r1.Ok {
		t.Fatalf("expected ok:true on first destroy")
	}
	r2, err := plugin.DestroyInstance(ctx, first.InstanceId)
	if err != nil {
		t.Fatalf("DestroyInstance #2 (idempotent): %v", err)
	}
	if !r2.Ok {
		t.Fatalf("expected ok:true on idempotent destroy")
	}
}

func TestProviderRegistryRegisterGetList(t *testing.T) {
	t.Parallel()

	reg := newRegistry()
	plugin := newMockContabo()

	if err := reg.Register(plugin); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(plugin); !errors.Is(err, schemas.ErrProviderAlreadyRegistered) {
		t.Fatalf("expected ErrProviderAlreadyRegistered, got %v", err)
	}

	got, err := reg.Get("contabo")
	if err != nil {
		t.Fatalf("Get(contabo): %v", err)
	}
	if got.Manifest().ProviderId != "contabo" {
		t.Fatalf("Get returned wrong plugin")
	}

	if _, err := reg.Get("nope"); !errors.Is(err, schemas.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}

	all := reg.List()
	if len(all) != 1 {
		t.Fatalf("expected 1 registered plugin, got %d", len(all))
	}
}

func TestManifestHasCapabilityMatchesDeclaredFlags(t *testing.T) {
	t.Parallel()

	plugin := newMockContabo()
	m := plugin.Manifest()

	if !m.HasCapability("vps.create") {
		t.Fatalf("expected vps.create to be declared")
	}
	if !m.HasCapability("vps.destroy") {
		t.Fatalf("expected vps.destroy to be declared")
	}
	if m.HasCapability("vps.snapshot") {
		t.Fatalf("did not expect vps.snapshot to be declared (mock didn't opt in)")
	}
}

func ptr[T any](v T) *T { return &v }
