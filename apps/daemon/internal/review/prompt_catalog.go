package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// hp-8xm engine slice: per-round prompt template catalog. The
// existing review.go ships RoundCatalog with 10 entries (rounds
// 0..9); this file adds the versioned prompt body each round
// renders into the agent runtime's context (direct-LLM mode) or
// the NTM marching-orders payload (delegated-agent mode).
//
// Template authority: this catalog is the single source of truth.
// The eventual packages/schemas/prompts/review-rounds/round-N.md
// files (per the bead's PROMPTS section) materialize from this
// catalog at codegen time; the daemon imports the catalog directly
// to avoid an extra YAML dependency. A schema-version event (per
// §10.3) bumps PromptCatalogSchemaVersion when any template body
// or version field changes.

// PromptCatalogSchemaVersion is bumped on every breaking template
// edit. The audit log records which version was rendered so old
// findings remain interpretable when templates evolve.
const PromptCatalogSchemaVersion = 1

// PromptVariable is a typed placeholder name the renderer
// substitutes at runtime. Templates reference these as
// `{{.<Name>}}`; missing variables fail RenderPrompt loudly so
// findings never carry empty placeholders.
type PromptVariable string

const (
	PromptVarProjectName       PromptVariable = "ProjectName"
	PromptVarBeadID            PromptVariable = "BeadID"
	PromptVarBeadTitle         PromptVariable = "BeadTitle"
	PromptVarReviewSubject     PromptVariable = "ReviewSubject"
	PromptVarRecentDiffSummary PromptVariable = "RecentDiffSummary"
	PromptVarHealthHotspots    PromptVariable = "HealthHotspots"
	PromptVarPriorFindings     PromptVariable = "PriorFindings"
	PromptVarAgentsMD          PromptVariable = "AgentsMD"
	PromptVarRoundIndex        PromptVariable = "RoundIndex"
)

// PromptTemplate is one row in the per-round catalog.
type PromptTemplate struct {
	// SchemaVersion of the catalog at the time this template was
	// frozen.
	SchemaVersion int `json:"schemaVersion"`

	// RoundID matches the existing RoundSpec.RoundID
	// (round-0..round-9) so the runner can join across catalogs.
	RoundID string `json:"roundId"`

	// RoundIndex matches RoundSpec.Index for ordering.
	RoundIndex int `json:"roundIndex"`

	// Body is the canonical prompt body. Variables are referenced
	// as `{{.VarName}}`; the renderer substitutes them.
	Body string `json:"body"`

	// RequiredVars lists the typed variables the body references.
	// RenderPrompt fails when any are missing from the input map.
	RequiredVars []PromptVariable `json:"requiredVars,omitempty"`

	// Version is a per-template revision string (e.g., "v1.2")
	// that bumps independently of the catalog SchemaVersion when
	// a single template's wording is iterated without changing
	// the variable contract.
	Version string `json:"version"`
}

// ErrMissingPromptVariable is returned by RenderPrompt when a
// required variable is missing or empty.
var ErrMissingPromptVariable = errors.New("review: prompt missing required variable")

// PromptCatalog returns the per-round prompt templates indexed
// by RoundID (round-0..round-9).
//
// Every body opens with the round's intent + the §1.4
// inspectability reminder + the per-round contract for what the
// reviewer must produce. Bodies are kept tight (≤ 30 lines) so
// the model context budget stays predictable; richer per-round
// guidance lives in vibing-with-ntm skills (loaded separately).
func PromptCatalog() []PromptTemplate {
	return []PromptTemplate{
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-0",
			RoundIndex:    0,
			Version:       "v1.0",
			Body: trim(`
You are running Round 0: UBS first-pass scan.

This round is deterministic — UBS is the source. Your job is to
invoke ubs --ci on the changed files, capture the result, and
emit each warning as a Finding with sourceTool="ubs".

Project: {{.ProjectName}}
Bead under review: {{.BeadID}} — {{.BeadTitle}}

Output: a list of findings, one per UBS warning. Stamp every
finding with the UBS rule ID, severity, and the file:line
location UBS reports. Do not paraphrase; copy UBS verbatim into
the finding body.
`),
			RequiredVars: []PromptVariable{
				PromptVarProjectName, PromptVarBeadID, PromptVarBeadTitle,
			},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-1",
			RoundIndex:    1,
			Version:       "v1.0",
			Body: trim(`
You are running Round 1: original-agent self-review.

You are the implementer of {{.BeadID}} — {{.BeadTitle}}. While
context is fresh, review your own landed work for: silent edge
cases you didn't cover, places where you took a shortcut you'd
flag in someone else's PR, and any commits that need a follow-up
bead instead of staying ambient.

Recent diff summary:
{{.RecentDiffSummary}}

Output: findings + 0..1 follow-up beads + a one-line confidence
summary. Honest [SILENT] is allowed if the work is clean.
`),
			RequiredVars: []PromptVariable{
				PromptVarBeadID, PromptVarBeadTitle, PromptVarRecentDiffSummary,
			},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-2",
			RoundIndex:    2,
			Version:       "v1.0",
			Body: trim(`
You are running Round 2: cross-agent review.

A peer agent landed {{.BeadID}} — {{.BeadTitle}}. You are NOT
the implementer. Read the bead's evidence, the diff, the test
output, and the prior findings; identify gaps the implementer
might have rationalized but a fresh peer would flag.

Prior findings on this bead:
{{.PriorFindings}}

Output: findings + recommended dispositions. Use the §7.4.3
triage actions when proposing follow-ups (fix_immediately /
new_bead / attach_blocker / false_positive / needs_human).
`),
			RequiredVars: []PromptVariable{
				PromptVarBeadID, PromptVarBeadTitle, PromptVarPriorFindings,
			},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-3",
			RoundIndex:    3,
			Version:       "v1.0",
			Body: trim(`
You are running Round 3: fresh-eyes new-session review.

Pretend you've never seen this project before. Read AGENTS.md +
README.md fresh, scan the bead's affected files, and report
anything that violates the project's own stated rules or the
twelve Hoopoe guardrails (Appendix C).

AGENTS.md excerpt:
{{.AgentsMD}}

Review subject: {{.ReviewSubject}}

Output: findings keyed to the rule each violates. Cite the
section number when possible. Do not propose code changes; just
flag.
`),
			RequiredVars: []PromptVariable{
				PromptVarAgentsMD, PromptVarReviewSubject,
			},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-4",
			RoundIndex:    4,
			Version:       "v1.0",
			Body: trim(`
You are running Round 4: random code exploration.

Pick three files in {{.ProjectName}} you haven't read yet from
the affected area. For each file, look for:
- inconsistencies with the rest of the package
- unused exports or dead code paths
- unhandled error returns
- TODO / FIXME / XXX without owners

Output: per-file finding list. Skip files that look clean.
`),
			RequiredVars: []PromptVariable{PromptVarProjectName},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-5",
			RoundIndex:    5,
			Version:       "v1.0",
			Body: trim(`
You are running Round 5: hotspot-targeted review.

The health adapter has flagged these hotspots:
{{.HealthHotspots}}

For each hotspot, confirm whether the problem is real (high
complexity + low coverage = needs refactor; high churn alone =
informational) and propose either a refactor finding or a
test-coverage finding. UBS rerun on these hotspots is allowed
and encouraged when the hotspot reason includes a UBS category.
`),
			RequiredVars: []PromptVariable{PromptVarHealthHotspots},
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-6",
			RoundIndex:    6,
			Version:       "v1.0",
			Body: trim(`
You are running Round 6: test/coverage hardening.

Look at the bead's affected files and identify untested paths.
For each missing path, propose either:
- a new test case (preferred — small, well-scoped) OR
- a coverage waiver with a documented reason

Run new tests in an isolated health worktree (Guardrail 5);
never inside the active agent working tree. Output: finding per
gap + the test snippet you propose.
`),
			RequiredVars: nil,
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-7",
			RoundIndex:    7,
			Version:       "v1.0",
			Body: trim(`
You are running Round 7: UI / UX polish.

The project has a renderer surface. Inspect the affected
components for: keyboard navigation gaps, ARIA attribute
omissions, focus-trap correctness inside dialogs, missing
"showing latest N of M" affordances on long lists (anti-pattern
#3), and any silent client-side caps.

Skip this round entirely if the bead's affected files are
back-end only.

Output: findings + suggested fixes; one per surface, not one
per element.
`),
			RequiredVars: nil,
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-8",
			RoundIndex:    8,
			Version:       "v1.0",
			Body: trim(`
You are running Round 8: specialized audits.

Pick the skills relevant to the bead's domain (security-audit
for auth code, deadlock-finder for concurrency code,
mock-code-finder for paths that should be live, profiling for
hot paths). Run each as a delegated agent with the skill loaded.

Each finding must stamp sourceTool with the skill name so the
finding ledger can dedupe across rounds.
`),
			RequiredVars: nil,
		},
		{
			SchemaVersion: PromptCatalogSchemaVersion,
			RoundID:       "round-9",
			RoundIndex:    9,
			Version:       "v1.0",
			Body: trim(`
You are running Round 9: final landing checklist.

Verify, in order:
1. All tests / builds green (rch exec -- ... where possible).
2. Code-health follow-up findings either fixed or filed as beads.
3. Git working tree clean and pushed to origin.
4. br state synced (br sync --flush-only) and committed.
5. Audit log shows every round artifact present.

Output: pass/fail per item with evidence pointer. If any fail,
do NOT mark landing complete; emit findings instead.
`),
			RequiredVars: nil,
		},
	}
}

// PromptByRoundID returns the template for the given round ID,
// or false on miss.
func PromptByRoundID(roundID string) (PromptTemplate, bool) {
	for _, t := range PromptCatalog() {
		if t.RoundID == roundID {
			return t, true
		}
	}
	return PromptTemplate{}, false
}

// PromptByRoundIndex returns the template for the given round
// index, or false on miss.
func PromptByRoundIndex(index int) (PromptTemplate, bool) {
	for _, t := range PromptCatalog() {
		if t.RoundIndex == index {
			return t, true
		}
	}
	return PromptTemplate{}, false
}

// RenderPrompt substitutes variables in the template's Body and
// returns the final string. Unknown variables in vars are
// ignored; missing required variables fail loudly.
//
// The substitution is a deliberate plain-string replace
// (`{{.Name}}` → vars[Name]) so the function has zero parsing
// overhead and zero runtime template-engine surface; the catalog
// is the security boundary.
func RenderPrompt(template PromptTemplate, vars map[PromptVariable]string) (string, error) {
	for _, required := range template.RequiredVars {
		val, ok := vars[required]
		if !ok || strings.TrimSpace(val) == "" {
			return "", fmt.Errorf("%w: %s", ErrMissingPromptVariable, required)
		}
	}
	rendered := template.Body
	for name, value := range vars {
		placeholder := "{{." + string(name) + "}}"
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}
	return rendered, nil
}

// PromptDigest returns a stable sha256 hex digest of a rendered
// prompt body. Audit log entries record this digest so future
// archeology can match a finding back to the exact prompt that
// produced it without storing the prompt body inline.
func PromptDigest(rendered string) string {
	sum := sha256.Sum256([]byte(rendered))
	return hex.EncodeToString(sum[:])
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
