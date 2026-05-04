// Package risks tracks the plan.md Section 14 risk register and the concrete
// acceptance evidence that keeps each mitigation from becoming stale prose.
package risks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion = 1
	ExpectedCount = 14
	PlanSection   = "plan.md Section 14"
)

var ErrInvalidCatalog = errors.New("risks: invalid catalog")

type Status string

const (
	StatusGreen Status = "green"
)

type EvidenceRef struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

type Risk struct {
	ID             int           `json:"id"`
	Slug           string        `json:"slug"`
	Title          string        `json:"title"`
	Owner          string        `json:"owner"`
	Status         Status        `json:"status"`
	PlanSection    string        `json:"planSection"`
	Mitigations    []string      `json:"mitigations"`
	Beads          []string      `json:"beads"`
	AcceptanceRefs []EvidenceRef `json:"acceptanceRefs"`
	ReleaseSmoke   bool          `json:"releaseSmoke"`
}

func Section14Catalog() []Risk {
	return cloneRisks(section14Catalog)
}

func ValidateCatalog(catalog []Risk) error {
	if len(catalog) != ExpectedCount {
		return fmt.Errorf("%w: got %d risks, want %d", ErrInvalidCatalog, len(catalog), ExpectedCount)
	}
	seen := map[int]struct{}{}
	for _, risk := range catalog {
		if risk.ID < 1 || risk.ID > ExpectedCount {
			return fmt.Errorf("%w: risk id %d out of range", ErrInvalidCatalog, risk.ID)
		}
		if _, exists := seen[risk.ID]; exists {
			return fmt.Errorf("%w: duplicate risk id %d", ErrInvalidCatalog, risk.ID)
		}
		seen[risk.ID] = struct{}{}
		if strings.TrimSpace(risk.Slug) == "" || strings.TrimSpace(risk.Title) == "" {
			return fmt.Errorf("%w: risk %02d missing slug/title", ErrInvalidCatalog, risk.ID)
		}
		if strings.TrimSpace(risk.Owner) == "" {
			return fmt.Errorf("%w: risk %02d missing owner", ErrInvalidCatalog, risk.ID)
		}
		if risk.Status != StatusGreen {
			return fmt.Errorf("%w: risk %02d status %q is not green", ErrInvalidCatalog, risk.ID, risk.Status)
		}
		if risk.PlanSection != PlanSection {
			return fmt.Errorf("%w: risk %02d plan section %q", ErrInvalidCatalog, risk.ID, risk.PlanSection)
		}
		if len(trimmed(risk.Mitigations)) == 0 {
			return fmt.Errorf("%w: risk %02d missing mitigations", ErrInvalidCatalog, risk.ID)
		}
		if len(trimmed(risk.Beads)) == 0 {
			return fmt.Errorf("%w: risk %02d missing bead references", ErrInvalidCatalog, risk.ID)
		}
		if len(risk.AcceptanceRefs) == 0 {
			return fmt.Errorf("%w: risk %02d missing acceptance evidence", ErrInvalidCatalog, risk.ID)
		}
		for idx, ref := range risk.AcceptanceRefs {
			if strings.TrimSpace(ref.Path) == "" || strings.TrimSpace(ref.Description) == "" {
				return fmt.Errorf("%w: risk %02d evidence %d incomplete", ErrInvalidCatalog, risk.ID, idx)
			}
		}
		if !risk.ReleaseSmoke {
			return fmt.Errorf("%w: risk %02d missing release smoke flag", ErrInvalidCatalog, risk.ID)
		}
	}
	for id := 1; id <= ExpectedCount; id++ {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("%w: missing risk id %02d", ErrInvalidCatalog, id)
		}
	}
	return nil
}

func cloneRisks(in []Risk) []Risk {
	out := make([]Risk, len(in))
	for i, risk := range in {
		out[i] = risk
		out[i].Mitigations = append([]string(nil), risk.Mitigations...)
		out[i].Beads = append([]string(nil), risk.Beads...)
		out[i].AcceptanceRefs = append([]EvidenceRef(nil), risk.AcceptanceRefs...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func trimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

var section14Catalog = []Risk{
	{
		ID:          1,
		Slug:        "pty-streaming-fidelity",
		Title:       "PTY streaming fidelity fails",
		Owner:       "daemon/process",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"NTM robot/stream surfaces are preferred before raw pane capture.",
			"Process and job-log state degrade to inspectable records instead of UI-critical terminal truth.",
		},
		Beads: []string{"hp-gkk", "hp-2qn"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/process/manager_test.go", Description: "process manager start/stop/adoption invariants"},
			{Path: "apps/daemon/internal/jobs/log/store_test.go", Description: "bounded job-log chunk retrieval for diagnostics"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          2,
		Slug:        "tool-output-drift",
		Title:       "Tool output drift breaks adapters",
		Owner:       "daemon/adapters",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Adapters prefer robot, API, or JSON surfaces.",
			"Capability reports expose missing, malformed, unsupported, timeout, and high-volume cases.",
		},
		Beads: []string{"hp-r33", "hp-dz8", "hp-g1j", "hp-yqs"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/adapters/br/br_test.go", Description: "br adapter contract and malformed-output coverage"},
			{Path: "apps/daemon/internal/capabilities/contract_test.go", Description: "capability registry contract gate"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          3,
		Slug:        "cache-diverges-from-canonical",
		Title:       "Hoopoe cache diverges from canonical state",
		Owner:       "daemon/projects",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Canonical tool state wins over daemon read models.",
			"Project gates and clone sync surfaces expose stale or missing canonical facts.",
		},
		Beads: []string{"hp-2n1", "hp-ilt", "hp-r33"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/clone/sync/sync_test.go", Description: "clone sync refuses local-write canonical drift"},
			{Path: "apps/daemon/internal/projects/gates/gates_test.go", Description: "project gates evaluate canonical facts before transitions"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          4,
		Slug:        "first-install-brittle",
		Title:       "First install is brittle",
		Owner:       "daemon/onboarding",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Existing VPS remains the default path.",
			"Bootstrap progress is checkpointed with raw-log fallback and repair actions.",
		},
		Beads: []string{"hp-qq9", "hp-wa3", "hp-4iz"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/onboarding/acfs/parser_test.go", Description: "ACFS marker parsing, fallback, and checkpoint emission"},
			{Path: "apps/daemon/internal/onboarding/checkpoints/checkpoints_test.go", Description: "resumable checkpoint timeline and repair hints"},
			{Path: "apps/daemon/internal/upgrade/service_test.go", Description: "daemon upgrade backup and rollback coverage"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          5,
		Slug:        "subscription-rate-limits",
		Title:       "Subscription rate-limits exhaust mid-swarm",
		Owner:       "daemon/inventory",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"caut and CAAM remain the subscription-budget/account surfaces.",
			"Inventory verification warns without inventing API-key quota accounting.",
		},
		Beads: []string{"hp-0ug", "hp-g1j", "hp-xr8"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/adapters/caut/client_test.go", Description: "subscription quota adapter contract"},
			{Path: "apps/daemon/internal/adapters/caam/client_test.go", Description: "CAAM account/profile detection contract"},
			{Path: "apps/daemon/internal/inventory/service_test.go", Description: "CAAM subscription verification warnings"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          6,
		Slug:        "build-test-contention",
		Title:       "Agents compete for builds/tests",
		Owner:       "daemon/scheduler",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"rch is the preferred build/test runner when configured.",
			"Scheduler and cleanup jobs keep contention and disk pressure inspectable.",
		},
		Beads: []string{"hp-5s4", "hp-6yw", "hp-4ya"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/adapters/rch/rch_test.go", Description: "rch argv safety and result classification"},
			{Path: "apps/daemon/internal/scheduler/scheduler_test.go", Description: "scheduler run/de-dupe invariants"},
			{Path: "apps/daemon/internal/tending/worktreecleanup/cleanup_test.go", Description: "disk-pressure cleanup planning"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          7,
		Slug:        "stale-agents-hold-work",
		Title:       "Stale agents hold beads/reservations hostage",
		Owner:       "daemon/tending",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Agent Mail remains the reservation source of truth.",
			"Typed action plans handle status prompts and force-release flows with audit.",
		},
		Beads: []string{"hp-0d7", "hp-209", "hp-5s4"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/adapters/agentmail/client_test.go", Description: "Agent Mail adapter reservation and message contract"},
			{Path: "apps/daemon/internal/tending/prescript/runner_test.go", Description: "prescript action-plan generation and silent tick policy"},
			{Path: "packages/tending-actions/test/loader.test.ts", Description: "force-release action schema defaults"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          8,
		Slug:        "unsafe-commands-exposed",
		Title:       "Unsafe commands accidentally exposed",
		Owner:       "daemon/security",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Mutation flows use typed action specs and allowlisted adapters.",
			"DCG, SLB, approvals, sandboxing, and audit all sit before execution.",
		},
		Beads: []string{"hp-209", "hp-ki3h", "hp-dmoj", "hp-rcm", "hp-kuh"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/agent/executor_test.go", Description: "typed action executor approval and postcondition gates"},
			{Path: "apps/daemon/internal/security/privsep/privsep_test.go", Description: "least-privilege setup-helper allowlist"},
			{Path: "apps/daemon/internal/clone/sandbox/sandbox_test.go", Description: "path sandboxing invariants"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          9,
		Slug:        "planning-quality-weak",
		Title:       "Planning quality is weak",
		Owner:       "daemon/beadflow",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Plan-to-bead traceability and quality dimensions make gaps inspectable.",
			"Lock and conversion gates block weak planning artifacts before swarm launch.",
		},
		Beads: []string{"hp-3ab", "hp-al4", "hp-ktn"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/beadflow/beadflow_test.go", Description: "traceability flags coverage gaps and risks"},
			{Path: "apps/daemon/internal/beadflow/conversion_test.go", Description: "conversion artifact and br workflow checks"},
			{Path: "apps/daemon/internal/projects/gates/gates_test.go", Description: "plan lock gate checks planning preconditions"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          10,
		Slug:        "subjective-scores-overtrusted",
		Title:       "Users trust subjective scores too much",
		Owner:       "daemon/review",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Scores stay paired with evidence, findings, and override/follow-up paths.",
			"Final gates can pass only with documented exceptions or follow-up beads.",
		},
		Beads: []string{"hp-sqd", "hp-r3l", "hp-mzg"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/beadflow/quality.go", Description: "quality dimensions retain findings beside scores"},
			{Path: "apps/daemon/internal/convergence/convergence_test.go", Description: "landing checklist requires evidence or follow-ups"},
			{Path: "apps/daemon/internal/review/review_test.go", Description: "review findings remain traceable through rounds"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          11,
		Slug:        "laptop-sleep-reliability",
		Title:       "Laptop sleep breaks perception of reliability",
		Owner:       "daemon/transport",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"The VPS daemon owns jobs and event streams.",
			"Reconnect uses sequence replay and snapshots instead of renderer memory.",
		},
		Beads: []string{"hp-e7k", "hp-2qn", "hp-gkk"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/transport/server_test.go", Description: "daemon transport and replay behavior"},
			{Path: "apps/daemon/internal/chaos/scenarios_test.go", Description: "chaos fixtures for disconnect/reconnect cases"},
			{Path: "docs/reconnect-replay.md", Description: "reconnect and replay contract"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          12,
		Slug:        "lifted-code-codex-assumptions",
		Title:       "Lifted code carries Codex-shaped assumptions",
		Owner:       "desktop/source-provenance",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Vendored t3code stays isolated and adapted through Hoopoe-owned wrappers.",
			"Codex/thread/provider/chat strings are scrubbed by an explicit check.",
		},
		Beads: []string{"hp-1.5", "hp-dtx"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "scripts/codex-shape-scrub/check-codex-shape-scrub.test.ts", Description: "Codex-shaped terminology scrub check"},
			{Path: "apps/desktop/tests/smoke/t3code-lift-smoke.test.ts", Description: "t3code lift smoke coverage"},
			{Path: "docs/source-provenance.md", Description: "source provenance policy"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          13,
		Slug:        "upstream-t3code-drift",
		Title:       "Upstream t3code drift",
		Owner:       "desktop/source-provenance",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Vendored source is pinned and reviewed deliberately.",
			"Security-relevant changes are cherry-picked instead of merged blindly.",
		},
		Beads: []string{"hp-1.5"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "docs/source-provenance.md", Description: "quarterly upstream review and cherry-pick policy"},
			{Path: "apps/desktop/src/vendored/t3code/README.md", Description: "vendored t3code provenance note"},
			{Path: "apps/desktop/src/vendored/t3code/LICENSE", Description: "preserved upstream license"},
		},
		ReleaseSmoke: true,
	},
	{
		ID:          14,
		Slug:        "unbounded-pubsub-patterns",
		Title:       "PubSub.unbounded patterns leak through",
		Owner:       "daemon/transport",
		Status:      StatusGreen,
		PlanSection: PlanSection,
		Mitigations: []string{
			"Daemon channels are bounded by design.",
			"Load tests exercise burst, slow-consumer, and wedged-consumer cases.",
		},
		Beads: []string{"hp-q3p"},
		AcceptanceRefs: []EvidenceRef{
			{Path: "apps/daemon/internal/transport/load/eventhub_burst_test.go", Description: "bounded eventhub burst test"},
			{Path: "apps/daemon/internal/transport/load/eventhub_slow_consumer_test.go", Description: "slow-consumer bounded-channel test"},
			{Path: "apps/daemon/internal/transport/load/eventhub_wedged_consumer_test.go", Description: "wedged-consumer load test"},
		},
		ReleaseSmoke: true,
	},
}
