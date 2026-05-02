# Hoopoe Phase 0/1 review findings

## Round 0 — p1 (FuchsiaStone)
- Scope: UBS `--ci` over the 25-bead diff (153 TS/TSX + 2 Go files; 405 reviewable files total).
- Result: 14 CRITICAL "loose equality" hits all `== null` idioms; 285 warnings (mostly informational); 1 HIGH-but-suspect `Math.random` in peer preload.ts subscriptionId.
- Critical fixed: `apps/desktop/src/main/SettingsAuditTrail.ts:199,207` + `packages/fixtures/src/validate.ts:180,196,243,250,258,313,325,333,341,367,373` — converted `== null`/`!= null` to `=== null || === undefined` (strict). 23/23 tests still pass.
- Cross-bead grep for non-null `==` returned ZERO real coercion bugs in the diff.
- Filed: 1 LOW (UBS false-positive class), 1 MEDIUM (subscriptionId Math.random), 0 HIGH/CRITICAL remaining.

## Round 0 — p4 (FuchsiaPond)
- Scope: UBS over renderer, ProjectRegistry, desktop smoke tests, and Playwright config.
- Result: exact directory-form UBS invocation failed; explicit scoped file-list UBS completed.
- Critical fixed: strict null/undefined check in `SettingsModel.readDotted`.
- Remaining UBS warnings: `SettingRow` term narrowing and switch-case reports triaged as false positives.
- Follow-up: no HIGH findings from UBS Round 0.

This document is the running ledger of review findings from the
post-convergence review of Hoopoe's Phase 0/1 work (~48 commits, 25
beads closed at the time of writing).

**Format.** Each finding is one section with this shape:

```markdown
## <area> — [CRITICAL/HIGH/MEDIUM/LOW] short title
**Where:** `path:line`   **Issue:** root-cause   **Suggested fix:** steps
**Reviewer:** <pane id>   **Round:** <round number>
```

**Severity.** CRITICAL → fix immediately. HIGH → fix if ≤30 min, else
file. MEDIUM/LOW → file only.

**Process.** After each round, append a 5-line summary at the **TOP** of
this file under a `## Round N — <reviewer>` heading.

---

## Renderer settings — [CRITICAL] loose nullish comparison in dotted setting resolver
**Where:** `apps/desktop/src/renderer/settings/SettingsModel.ts:349`   **Issue:** `readDotted()` used `cur == null`, which UBS correctly treats as a critical loose-equality pattern even though the intent was a nullish guard.   **Suggested fix:** Replaced it with explicit `cur === null || cur === undefined` and reran UBS plus focused renderer settings/shell tests.   **Reviewer:** p4   **Round:** 0

## SettingsAuditTrail + fixtures validator — [CRITICAL] loose nullish (`== null`) in 12 places
**Where:** `apps/desktop/src/main/SettingsAuditTrail.ts:199,207`; `packages/fixtures/src/validate.ts:180,196,243,250,258,313,325,333,341,367,373`   **Issue:** All `== null`/`!= null` idioms — TS-stylistic shorthand for "null OR undefined" but UBS gates on strict-equality and the project lint should match.   **Suggested fix:** Replaced inline with explicit `=== null || === undefined`; tests still pass (23/23 fixtures + audit). Committed.   **Reviewer:** p1   **Round:** 0

## peer preload.ts — [MEDIUM] subscriptionId uses Math.random — UBS flag, not a real risk
**Where:** `apps/desktop/electron/preload.ts:106` — `const subscriptionId = \`${topic}#${Date.now()}-${Math.random().toString(36).slice(2)}\``   **Issue:** UBS pattern-matched on "subscription" + Math.random and flagged as a security-relevant token. The variable is an IPC subscription correlation id, not an auth token — Math.random is fine for uniqueness here.   **Suggested fix:** Optional cosmetic switch to `crypto.randomUUID()` for consistency, but no functional change required. File-only — no fix.   **Reviewer:** p1   **Round:** 0

## UBS rule discipline — [LOW] `== null` flagged as CRITICAL is a false-positive class for TS code
**Where:** UBS scanner module (out-of-repo)   **Issue:** TypeScript idiom `x == null` is intentional shorthand for the nullish union check; treating it as CRITICAL inflates the noise floor. The project lint should converge on either ESLint `eqeqeq` (with `null` exception) or `strict-equality everywhere` — pick one.   **Suggested fix:** Decide on project-wide policy in a Phase 2.5 ADR; until then, fix sites as UBS finds them (which is what we did).   **Reviewer:** p1   **Round:** 0
