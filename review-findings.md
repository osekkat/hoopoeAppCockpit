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

## Round 0 — p2 (GreenBear)
- Scope: UBS + security greps over Phase 1 t3code lift (`apps/desktop/src/vendored/t3code/**`), the hp-zir main/* decomp (BackendLifecycle / UpdateMachine / IpcRegistry / WindowManager / SettingsBridge / AuthBridge), hp-spx SecretStore, hp-rflj `apps/desktop/electron/preload.ts` + `.eslintrc.cjs`, and the lint scripts (codex-shape-scrub / providerlint / rendererlint).
- UBS: 0 critical attributable to my code; lint scripts + vendored t3code clean (vendored has 6 expected `JSON.parse` warnings — intentional fallback-on-throw); the SettingsAuditTrail.ts `== null` criticals are p1/p4's hp-wg5p scope, already fixed by them.
- Security greps clean: Guardrail 11 hits are all denylist patterns in fixture scanners; Guardrail 12 zero matches; Guardrail 2 hits are `RegExp.exec()` not shell-exec.
- **Fixed inline (Round 0 critical-fix window):** `apps/desktop/electron/preload.ts:106` `Math.random()` subscriptionId → `crypto.randomUUID()` (UBS independently flagged; p1 had filed as MEDIUM; I treated as HIGH given the channel-name binding and inlined the fix). UBS now clean for `apps/desktop/electron/`.
- Filed 4 HIGH (preload allowlist gaps for daemon.subscribe/request; AuthBridgeRedactedError token-shape too narrow; IpcRegistry generic-dispatch type-safety illusory pending hp-r3i) + 4 MEDIUM/LOW (daemonBaseUrl loopback-validation; HoopoeBridge unconstrained generics; channel-name format consistency; AuthBridge no retries).

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

## Renderer preload — [HIGH] Math.random subscriptionId folded into IPC channel name
**Where:** `apps/desktop/electron/preload.ts:106` (now fixed in this commit; was at that line before the Round 0 inline fix)   **Issue:** The renderer-controlled `topic` argument was string-interpolated into a subscriptionId together with `Math.random().toString(36).slice(2)`, then folded into the actual IPC channel name. Math.random is not cryptographically random; collisions or prediction across subscriptions could let a buggy or malicious renderer subscribe to another's channel. UBS independently flagged this ("Security token generated with Math.random") on the same line.   **Suggested fix:** Replace with `crypto.randomUUID()` so the channel suffix is unguessable and collision-free; keep `topic` as a diagnostic field only (not part of the channel binding). Done inline.   **Reviewer:** p2   **Round:** 0

## Renderer preload — [HIGH] no allowlist gate on `daemon.request(method, body)`
**Where:** `apps/desktop/electron/preload.ts:103-104` (`daemon.request` definition)   **Issue:** The renderer chooses the `method` string verbatim and the preload forwards it to main as `{ method, body }`. There is no preload-side allowlist; any string the renderer constructs reaches the main-process handler. If the main-process IpcRegistry handler doesn't independently validate `method` against an enumerated set, the renderer can drive arbitrary daemon methods.   **Suggested fix:** When `packages/schemas/preload-api.yaml` lands (hp-r3i, Phase 2.5), generate the legal-method literal-union type and refuse anything outside it at preload boundary; until then, document the gap inline at the call site so reviewers don't assume the boundary validates.   **Reviewer:** p2   **Round:** 0

## Renderer preload — [HIGH] no allowlist gate on `daemon.subscribe(topic, listener)`
**Where:** `apps/desktop/electron/preload.ts:105-114` (`daemon.subscribe` definition)   **Issue:** Same shape as `daemon.request`: renderer-supplied `topic` flows directly into the main-process subscribe handler. If the main handler doesn't enforce an allowlist, the renderer can subscribe to arbitrary topics — including topics the daemon emits internally (audit, redaction, settings-change events) that the renderer isn't entitled to observe.   **Suggested fix:** Generate the legal-topic literal-union type from `packages/schemas/` (hp-r3i); refuse unknown topics at the preload boundary; document the gap until then.   **Reviewer:** p2   **Round:** 0

## Auth — [HIGH] AuthBridgeRedactedError token-shape detection is too narrow
**Where:** `apps/desktop/src/main/AuthBridge.ts:39`   **Issue:** The constructor refuses messages containing `"eyJ"` (JWT prefix) or `"hp-bearer-"` (test-fixture prefix). Real Hoopoe daemon bearer tokens are HMAC-signed (per `plan.md §2.6` `SessionCredentialService`) and look like url-safe base64 — they don't start with `eyJ`. A future error-path change that interpolates the bearer into the message would PASS the current guard. UBS doesn't flag this because it's a custom guard, not a stdlib pattern.   **Suggested fix:** Replace the substring check with a regex that catches any opaque string of length ≥ 24 with high entropy (e.g., `/[A-Za-z0-9_-]{24,}/`). Add a unit test that constructs `AuthBridgeRedactedError` with a synthetic HMAC-shaped token and asserts it throws the meta-error. Also add a `// codex-shape-scrub-ok:` style comment to whitelist `eyJ` / `hp-bearer-` mentions in the guard's source.   **Reviewer:** p2   **Round:** 0

## IPC — [HIGH] dispatch type-safety is illusory pending hp-r3i schemas
**Where:** `apps/desktop/src/main/IpcRegistry.ts:87-103` (`dispatch<Input, Output>`)   **Issue:** `dispatch<Input, Output>(commandId, input, context)` lets the caller specify both `Input` and `Output` as type parameters; TypeScript can't prove the registered handler's actual signature matches. So every IPC call site implicitly downcasts `unknown → Output`. This is a known gap (the bead docs reference hp-r3i's schemas as the closer), but the gap isn't documented inline at the dispatch surface, so future contributors may assume the registry is type-safe end-to-end.   **Suggested fix:** Add a doc-comment block above `dispatch` explicitly stating the gap and pointing at hp-r3i; when hp-r3i lands, replace the unconstrained generics with a discriminated-union channel map keyed by command id. File a follow-up bead to track the migration.   **Reviewer:** p2   **Round:** 0

## Auth — [MEDIUM] daemonBaseUrl is not loopback-validated before pairing exchange
**Where:** `apps/desktop/src/main/AuthBridge.ts:55-87`   **Issue:** `exchangePairingForBearer` and `issueWsToken` take `daemonBaseUrl` from caller-controlled input and POST the pairing token / Authorization header to it. Hoopoe's threat model assumes the daemon is reachable only on `127.0.0.1:*` / `localhost:*` (matches `ALLOWED_NAVIGATION_ORIGINS` from hp-rflj). If a misconfigured renderer or a settings-corruption bug ever populates `daemonBaseUrl` with an external host, the pairing token + bearer header leak to that host before the network layer notices.   **Suggested fix:** Reject any `daemonBaseUrl` whose origin isn't in the `ALLOWED_NAVIGATION_ORIGINS` set before issuing fetch; throw `AuthBridgeRedactedError` with a non-token-shaped message. Add a unit test that asserts an external host is rejected without performing the fetch.   **Reviewer:** p2   **Round:** 0

## Renderer preload — [MEDIUM] HoopoeBridge methods use unconstrained `<I, O>` generics
**Where:** `apps/desktop/electron/preload.ts:74-99`   **Issue:** Every method on `HoopoeBridge` declares `<I, O>` with no constraints, so renderer call sites can pass any input shape and assign the result to any type. This compiles cleanly but offers no compile-time correctness. Same root cause as the IpcRegistry dispatch finding — the closer is hp-r3i Phase 2.5 schemas — but the renderer-side gap is the bigger surface because the renderer is the security boundary.   **Suggested fix:** Replace per-method generics with concrete typed shapes once hp-r3i ships the generated client; until then, document the gap in the file's preamble so future PRs don't lean on the apparent type safety.   **Reviewer:** p2   **Round:** 0

## Renderer preload — [LOW] approvals.list channel name is `list-pending` while siblings are bare verbs
**Where:** `apps/desktop/electron/preload.ts:41-44`   **Issue:** `approvals.list-pending` is the only kebab-suffixed approvals channel (`approve` / `deny` / `extend` are bare verbs). Cosmetic inconsistency; future PRs may copy the inconsistency or invent a new pattern.   **Suggested fix:** Rename to `hoopoe.approvals.list` for consistency with the other approval channels; update the renderer-side method name `listPending → list` if no callers depend on it yet. Defer until hp-r3i locks the channel inventory.   **Reviewer:** p2   **Round:** 0

## Auth — [LOW] no retry/backoff on bootstrap exchange
**Where:** `apps/desktop/src/main/AuthBridge.ts:55-74`   **Issue:** A single transient network failure during the initial pairing exchange forces the user to re-enter the pairing token. Acceptable for a one-shot exchange but a bumpy onboarding bug for users on flaky networks.   **Suggested fix:** Defer to a Phase 2 wrapper that handles retries with bounded backoff (3 attempts, 200/600/1500ms); AuthBridge itself stays single-shot. File against Phase 2 connection-FSM bead.   **Reviewer:** p2   **Round:** 0
