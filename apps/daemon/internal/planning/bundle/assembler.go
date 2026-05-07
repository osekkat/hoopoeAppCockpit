// assembler.go — Builder.Build orchestration call-graph
// (hp-rsly fourteenth slice).
//
// `AssemblyOrchestrator` wires the engine-layer primitives shipped
// across hp-rsly slices 1-13 into a single Build call:
//
//   WalkProjectRoot
//     → SummarizeBeads + RankHotspots (adapter inputs)
//     → ApplyPolicy   (per-file gates)
//     → EnforceBudget (token-budget hard cap)
//     → ComputeContentHash + ComputeCacheKey
//     → returned bundle
//
// The orchestrator is BrAdapter / HealthAdapter agnostic: the caller
// supplies pre-fetched RawBead and RawHotspot slices. Keeping the
// wiring adapter-free means the existing-bead and hotspot follow-up
// tests don't need to spin up `br` or the health pipeline; the
// future BrAdapter / HealthAdapter integration slice plugs in by
// passing real adapter output into the orchestrator.
//
// What this slice does NOT do (still hp-rsly residual):
//
//   - Concrete BrAdapter / HealthAdapter implementations (those
//     exist in their own packages — beadflow + health — and will
//     wrap their adapter output into RawBead / RawHotspot when
//     invoked from the planning pipeline).
//   - HTTP handler wiring at /v1/projects/{id}/planning/context-bundle.
//   - UI artifact rail bundle viewer + truncation banner.

package bundle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// AssemblyInput captures the orchestrator's per-call inputs that
// don't come from the project root. The BrAdapter and HealthAdapter
// integration layer feeds these in alongside the BuildOpts so the
// orchestrator stays adapter-agnostic.
type AssemblyInput struct {
	// Beads is the BrAdapter output (or empty when the project has
	// no beads yet — common in early Phase 0 use). Truncation +
	// ranking happens inside the orchestrator via SummarizeBeads.
	Beads []RawBead

	// Hotspots is the HealthAdapter output (or empty when no health
	// snapshot has been computed yet). Truncation + ranking happens
	// inside the orchestrator via RankHotspots.
	Hotspots []RawHotspot

	// Policy lets the caller override the §5.5 default policy for
	// per-project model-context overrides. Pass nil to use
	// DefaultPolicy().
	Policy *Policy
}

// AssemblyOrchestrator implements Builder using the engine-layer
// primitives. Construct via NewAssemblyOrchestrator; the zero value
// is not safe for use because the discovery walk needs an absolute
// project-root path resolved per Build call.
type AssemblyOrchestrator struct {
	clock func() time.Time
}

// NewAssemblyOrchestrator returns an orchestrator with the wall
// clock for GeneratedAt stamping. Tests can inject a fixed clock by
// constructing the struct literal directly.
func NewAssemblyOrchestrator() *AssemblyOrchestrator {
	return &AssemblyOrchestrator{clock: time.Now}
}

// Build wires all primitives together. This is the engine-layer
// implementation of the Builder interface from bundle.go. The
// adapter integration slice can wrap this in a higher-level
// `Builder` that calls into BrAdapter / HealthAdapter to populate
// AssemblyInput.
func (o *AssemblyOrchestrator) Build(ctx context.Context, opts BuildOpts, input AssemblyInput) (*schemas.ExistingCodebaseContextBundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// 1. Discovery walk — captures README, AGENTS, arch docs,
	//    manifests, test layout from disk.
	walk, err := WalkProjectRoot(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: walk: %w", err)
	}

	// 2. Bead/hotspot summaries — adapter outputs piped through the
	//    deterministic summarizers.
	beadSummaries, err := SummarizeBeads(input.Beads, 0)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: beads: %w", err)
	}
	hotspotSummaries, err := RankHotspots(input.Hotspots, 0)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: hotspots: %w", err)
	}

	// 3. Apply §5.5 path policy — every captured path runs through
	//    DefaultPolicy (or the per-project override) so secrets-
	//    suggestive paths don't reach the model.
	policy := input.Policy
	if policy == nil {
		p := DefaultPolicy()
		policy = &p
	}
	excluded := []string{}
	walk, excluded, err = applyPolicyToWalk(walk, policy)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: policy: %w", err)
	}
	excluded = append(excluded, walk.SkippedArchitectureDocs...)

	// 4. Stitch the bundle. CommitSha + ProjectId come from BuildOpts;
	//    GeneratedAt is stamped here so it's deterministic-relative
	//    to the Build call (the cache layer ignores it via
	//    ComputeContentHash).
	bundle := &schemas.ExistingCodebaseContextBundle{
		ProjectId:        opts.ProjectID,
		CommitSha:        opts.CommitSHA,
		SchemaVersion:    schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion),
		Readme:           walk.Readme,
		AgentsMd:         walk.AgentsMd,
		ArchitectureDocs: walk.ArchitectureDocs,
		PackageManifests: walk.PackageManifests,
		TestLayout:       walk.TestLayout,
		ExistingBeads:    beadSummaries,
		HealthHotspots:   hotspotSummaries,
		Excluded:         excluded,
		Redactions:       []schemas.RedactionEntry{},
		TokenBudget:      opts.TokenBudget,
		GeneratedAt:      o.now(),
	}

	// 5. Token-budget enforcement — drops lower-priority sections
	//    until the estimate fits. Updates TokenEstimate field +
	//    extends Excluded with synthetic markers.
	_, dropped, err := EnforceBudget(bundle, opts.TokenBudget)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: budget: %w", err)
	}
	bundle.Excluded = append(bundle.Excluded, dropped...)

	// 6. Content-addressable hash — populated last so the hash is
	//    over the truncated, policy-filtered, finalized bundle.
	hash, err := ComputeContentHash(*bundle)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: hash: %w", err)
	}
	bundle.ContentHash = hash

	return bundle, nil
}

// applyPolicyToWalk drops every captured surface whose path fails
// the policy. README/AGENTS/each arch-doc/each manifest is checked
// individually; rejected entries become excluded markers (with the
// PolicyDecision reason inline) and are removed from the walk.
func applyPolicyToWalk(walk *DiscoveryResult, policy *Policy) (*DiscoveryResult, []string, error) {
	excluded := []string{}

	pathsToCheck := []string{}
	if walk.Readme != nil {
		pathsToCheck = append(pathsToCheck, walk.Readme.Path)
	}
	if walk.AgentsMd != nil {
		pathsToCheck = append(pathsToCheck, walk.AgentsMd.Path)
	}
	for _, d := range walk.ArchitectureDocs {
		pathsToCheck = append(pathsToCheck, d.Path)
	}
	for _, m := range walk.PackageManifests {
		pathsToCheck = append(pathsToCheck, m.Path)
	}

	decisions, err := ApplyPolicy(pathsToCheck, policy)
	if err != nil {
		return nil, nil, err
	}

	rejected := map[string]string{}
	for _, d := range decisions {
		if !d.Admitted {
			rejected[d.Path] = d.Reason
			excluded = append(excluded, d.Path+" ["+d.Reason+"]")
		}
	}

	if walk.Readme != nil {
		if _, ok := rejected[walk.Readme.Path]; ok {
			walk.Readme = nil
		}
	}
	if walk.AgentsMd != nil {
		if _, ok := rejected[walk.AgentsMd.Path]; ok {
			walk.AgentsMd = nil
		}
	}
	walk.ArchitectureDocs = filterArch(walk.ArchitectureDocs, rejected)
	walk.PackageManifests = filterManifests(walk.PackageManifests, rejected)

	return walk, excluded, nil
}

func filterArch(in []schemas.FileSnapshot, rejected map[string]string) []schemas.FileSnapshot {
	out := in[:0]
	for _, d := range in {
		if _, dropped := rejected[d.Path]; dropped {
			continue
		}
		out = append(out, d)
	}
	// Avoid sharing backing array with caller's empty case.
	if len(out) == 0 {
		return []schemas.FileSnapshot{}
	}
	return out
}

func filterManifests(in []schemas.ManifestSnapshot, rejected map[string]string) []schemas.ManifestSnapshot {
	out := in[:0]
	for _, m := range in {
		if _, dropped := rejected[m.Path]; dropped {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return []schemas.ManifestSnapshot{}
	}
	return out
}

func (o *AssemblyOrchestrator) now() time.Time {
	if o == nil || o.clock == nil {
		return time.Now()
	}
	return o.clock()
}

// Compile-time interface check: AssemblyOrchestrator does NOT
// satisfy the original Builder interface from bundle.go because
// Build now takes an extra AssemblyInput argument. The Builder
// interface there is for the legacy notImplementedBuilder; the
// adapter-integration slice will replace it with one that calls
// AssemblyOrchestrator.Build internally after fetching adapter
// output.
var _ = errors.New("planning/bundle: AssemblyOrchestrator does not satisfy Builder; the adapter-integration slice wraps it")
