// Package bundle implements the daemon-side existing-codebase context
// bundle assembly per plan.md §7.1 (third sub-mode: when the project's
// repo is non-empty, attach a bundle of README, AGENTS.md, architecture
// docs, package manifests, test layout, existing beads, and health
// hotspots to the prompts every candidate-model sees).
//
// The shape of the bundle is pinned by `packages/schemas/openapi.yaml`
// → `ExistingCodebaseContextBundle` (hp-rsly schema slice 1f61082);
// the Go binding lives in `packages/schemas/go/schemas.gen.go`.
//
// This file ships the Go interface skeleton so consumers (the planning
// pipeline, the diagnostics endpoint, refinement-round prompts) can
// import the contract and write tests against it ahead of the assembly
// implementation. The implementation is split across follow-up beads
// per the original hp-rsly DOD:
//
//   - Discovery walk (README/AGENTS/architecture-docs/manifests; cap depth)
//   - Test-layout detection per ecosystem
//   - BrAdapter integration (truncated bead summaries)
//   - HealthAdapter integration (top-25 hotspots ranking)
//   - Model-context policy enforcement (§5.5; secret scan + path rules)
//   - Token budget hard cap with documented truncation order
//   - Per-model serialization (CLI runners + Oracle)
//   - Content-addressable cache with LRU 30d eviction
//
// Until those follow-ups land, every Builder method returns
// ErrAssemblyNotImplemented. That keeps Hoopoe explicit about the
// cockpit-vs-engine boundary (Guardrail 4): the renderer must NOT
// consume fixture content as canonical bundle output.
package bundle

import (
	"context"
	"errors"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// SchemaVersion mirrors the openapi.yaml `ExistingCodebaseContextBundle.schemaVersion`
// field. Bump only when the bundle shape changes incompatibly; the
// content-addressable cache invalidates on schema bumps.
const SchemaVersion = 1

// ErrAssemblyNotImplemented is returned by every Builder method until
// the follow-up DOD beads land. Consumers can pattern-match this with
// `errors.Is` to surface a "Phase 5 bundle assembly pending" diagnostic
// instead of a generic 500.
var ErrAssemblyNotImplemented = errors.New("planning/bundle: assembly subsystem not yet implemented (hp-rsly residuals pending)")

// BuildOpts captures the inputs the assembly subsystem needs. Fields
// are required unless the doc comment says otherwise.
type BuildOpts struct {
	// ProjectID is the stable Hoopoe project identifier the bundle is
	// assembled for. Required.
	ProjectID string

	// ProjectRoot is the absolute filesystem path of the VPS-side
	// project working directory the daemon assembles from. Required.
	ProjectRoot string

	// CommitSHA is the Git commit the bundle is pinned to. Required.
	// The discovery walk reads files at this SHA; refinement-round
	// prompts always reference the same source-of-truth even if the
	// working tree advances.
	CommitSHA string

	// TokenBudget is the per-prompt token ceiling. The assembly
	// pipeline truncates lower-priority sections (existing beads →
	// arch docs → manifests) when the assembled estimate exceeds the
	// budget. 0 means "use builder default" per §5.5 model-context
	// policy.
	TokenBudget int
}

// Builder is the interface every bundle-assembly implementation
// satisfies. The default implementation in this package returns
// ErrAssemblyNotImplemented for every call; the follow-up beads
// replace it with a real walker / adapter integration / cache pipeline.
//
// Build is idempotent: calling it twice with the same BuildOpts must
// produce a bundle whose ContentHash is identical. The assembly
// subsystem caches by ContentHash so refinement rounds reuse the same
// bundle without re-walking the project root.
type Builder interface {
	Build(ctx context.Context, opts BuildOpts) (*schemas.ExistingCodebaseContextBundle, error)
}

// NewBuilder returns the package default Builder. Today every method
// returns ErrAssemblyNotImplemented; follow-up beads replace this with
// the real implementation.
func NewBuilder() Builder {
	return &notImplementedBuilder{}
}

type notImplementedBuilder struct{}

func (notImplementedBuilder) Build(ctx context.Context, opts BuildOpts) (*schemas.ExistingCodebaseContextBundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return nil, ErrAssemblyNotImplemented
}

// ErrInvalidOpts is returned when BuildOpts misses a required field.
// Reported separately from ErrAssemblyNotImplemented so callers can
// distinguish "your inputs are wrong" from "the engine isn't built
// yet."
var ErrInvalidOpts = errors.New("planning/bundle: invalid BuildOpts")

func (o BuildOpts) validate() error {
	if o.ProjectID == "" {
		return errInvalidOpts("ProjectID is required")
	}
	if o.ProjectRoot == "" {
		return errInvalidOpts("ProjectRoot is required")
	}
	if o.CommitSHA == "" {
		return errInvalidOpts("CommitSHA is required")
	}
	if len(o.CommitSHA) != 40 {
		return errInvalidOpts("CommitSHA must be a 40-char Git SHA")
	}
	if o.TokenBudget < 0 {
		return errInvalidOpts("TokenBudget must be non-negative")
	}
	return nil
}

func errInvalidOpts(msg string) error {
	// Wrap so callers can `errors.Is(err, ErrInvalidOpts)`.
	return &invalidOptsError{msg: msg}
}

type invalidOptsError struct {
	msg string
}

func (e *invalidOptsError) Error() string {
	return ErrInvalidOpts.Error() + ": " + e.msg
}

func (e *invalidOptsError) Is(target error) bool {
	return target == ErrInvalidOpts
}

func (e *invalidOptsError) Unwrap() error {
	return ErrInvalidOpts
}
