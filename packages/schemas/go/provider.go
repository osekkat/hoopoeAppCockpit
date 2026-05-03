// provider.go — handwritten Go interface for the abstract provider plugin
// contract per `packages/schemas/provider-plugin.yaml`.
//
// The DATA TYPES (Region, Size, CreateInstanceOpts, Instance,
// DestroyResult, EstimateCostOpts, CostEstimate, CostLineItem, Manifest)
// are generated from `openapi.yaml` into `schemas.gen.go`. This file adds
// the Go INTERFACE that wires the methods to those data types so the
// daemon's plugin loader (apps/daemon/internal/providers/, owned by hp-9fo)
// can implement the abstract contract once and let v1 ship with Contabo
// while leaving the door open for Hetzner / DigitalOcean / OVH / Linode.
//
// ## Contract invariants (mirrored from provider-plugin.yaml)
//
//   - Manifest() must declare every method the plugin opts into via
//     ManifestCapabilities. The daemon refuses to call an undeclared
//     method (returning a `provider.method_unsupported` problem+json).
//   - CreateInstance MUST clean up on failure — no orphaned billable
//     resources. If the provider partially created, the plugin destroys
//     before returning the error.
//   - DestroyInstance MUST be idempotent: a second call against an
//     already-destroyed ID returns `&DestroyResult{Ok: true,
//     InstanceId: id}` (not an error).
//   - EstimateMonthlyCost is a pure function: same (region, size,
//     bandwidthTBExpected) → same result for the same `CatalogVersion`.
//
// ## Mirror discipline
//
// Adding a new method here requires a coordinated update to:
//   1. provider-plugin.yaml (new `methods:` entry).
//   2. openapi.yaml (new component schema if it carries new types).
//   3. Manifest.capabilities enum in openapi.yaml.
//   4. Daemon's plugin registry to expose the new HTTP endpoint.
package schemas

import (
	"context"
	"fmt"
)

// ProviderPlugin is the abstract VPS-provisioning contract every provider
// plugin must implement. Plugins ship as Go packages compiled into the
// daemon binary and register themselves via package-init.
//
// All methods accept `ctx` so the daemon can cancel long-running calls
// (provisioning often takes minutes). Implementations must respect
// `ctx.Done()` and return promptly when cancelled.
type ProviderPlugin interface {
	// Manifest returns the plugin's self-description. Called once at
	// registration; the manifest must be deterministic (no random IDs).
	Manifest() ProviderPluginManifest

	// ListRegions enumerates the geographic regions the provider supports.
	// Pure read-only.
	ListRegions(ctx context.Context) ([]ProviderRegion, error)

	// ListSizes enumerates the instance sizes available in `regionID`.
	// `regionID` MUST be a value returned by ListRegions.
	ListSizes(ctx context.Context, regionID string) ([]ProviderSize, error)

	// EstimateMonthlyCost computes the expected monthly bill for a
	// (region, size, bandwidthTBExpected) tuple. Pure function: result
	// must be deterministic for a fixed `CatalogVersion` (the result
	// itself records that version).
	EstimateMonthlyCost(ctx context.Context, opts ProviderEstimateCostOpts) (*ProviderCostEstimate, error)

	// CreateInstance provisions a new VPS instance. Billable.
	// Implementations MUST clean up on failure (no orphaned billable
	// resources).
	CreateInstance(ctx context.Context, opts ProviderCreateInstanceOpts) (*ProviderInstance, error)

	// DestroyInstance destroys the named instance. Irreversible.
	// Idempotent: a second call against an already-destroyed ID returns
	// `{Ok: true, InstanceId: id}` (not an error).
	DestroyInstance(ctx context.Context, instanceID string) (*ProviderDestroyResult, error)
}

// ProviderRegistry holds the in-process catalog of registered plugins.
// v1 ships with Contabo only; the registry is open for future plugins
// without daemon-binary changes (registered via `init()` in plugin
// packages that the daemon imports for side effects).
type ProviderRegistry interface {
	// Register adds a plugin. Returns an error if a plugin with the same
	// `Manifest().ProviderId` is already registered.
	Register(plugin ProviderPlugin) error

	// Get returns the plugin registered under `id`. Returns
	// ErrProviderNotFound if no plugin is registered under that ID.
	Get(id ProviderId) (ProviderPlugin, error)

	// List returns every registered plugin in registration order.
	List() []ProviderPlugin
}

// ErrProviderNotFound is returned by ProviderRegistry.Get when the
// requested provider isn't registered.
var ErrProviderNotFound = fmt.Errorf("provider plugin not registered")

// ErrProviderAlreadyRegistered is returned by ProviderRegistry.Register
// when the manifest's ProviderId clashes with an existing plugin.
var ErrProviderAlreadyRegistered = fmt.Errorf("provider plugin already registered")

// ErrProviderMethodUnsupported is the error a plugin should return when
// asked to invoke a method it didn't declare in
// Manifest.Capabilities. The daemon translates this to a
// `provider.method_unsupported` problem+json.
var ErrProviderMethodUnsupported = fmt.Errorf("provider method not declared in manifest")

// HasCapability returns true iff the manifest declares the named
// capability flag. Used by the daemon's HTTP wrapper to short-circuit
// before invoking a plugin method.
func (m ProviderPluginManifest) HasCapability(flag ProviderPluginManifestCapabilities) bool {
	for _, c := range m.Capabilities {
		if c == flag {
			return true
		}
	}
	return false
}
