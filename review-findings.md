# Hoopoe Phase 0/1 review findings

## Round 4 — p3 (BlueHill)
- Scope: saturation pass over p3 surfaces after filing Round 3 goldens.
- Result: no new findings; forbidden provider SDK/API-key grep was clean in p3 scope.
- Confirmed: TerminalPane remains diagnostics-only and is not mounted in the default Swarm renderer.
- Saturation: Round 3 had 1 new finding and Round 4 had 0; p3 review saturation reached.

## Round 3 — p3 (BlueHill)
- Scope: `testing-golden-artifacts` pass for design-system primitive HTML shapes.
- Result: no committed golden/snapshot artifacts were found under `packages/design-system`.
- New findings: 1 MEDIUM covering the missing canonical DOM regression harness.
- Action: file-only per severity; no component behavior changed in this round.

## Round 2 — p3 (BlueHill)
- Scope: hp-z1x routed shell and hp-x14e top-bar/store cross-review.
- Result: route typing is acceptable; command-palette integration and Activity restoration still have gaps.
- New findings: 1 HIGH and 1 MEDIUM; storage failure hardening is already covered by p4 Round 1 and not duplicated.
- Action: filed findings only for shell/UI issues; no routed-shell edits applied.

## Round 1 — p3 (BlueHill)
- Scope: design-system primitives, hp-j30 tests, and release pipeline audit.
- Result: primitive story/test inventory is complete; TerminalPane diagnostics-only guardrail looks enforced.
- New findings: 1 HIGH, 3 MEDIUM, 1 LOW; the release HIGH was fixed inline in `.github/workflows/release.yml`.
- Action: filed remaining non-critical issues; no modal/component behavior changed.

## Round 0 — p3 (BlueHill)
- Scope: UBS over `packages/design-system`, `apps/desktop/tests`, release workflow, and desktop artifact scripts.
- Result: directory-form UBS failed on `apps/desktop/tests/`; explicit scoped file-list UBS completed with 0 critical findings.
- Triage: warnings were mostly static-token/story deep-property noise, function-in-block style, ternaries, and listener-balance heuristics.
- Action: no UBS-specific fixes; actionable overlaps are filed in later p3 rounds below.

## Round 1 — p1 (FuchsiaStone)
- Scope: determinism (rebuild scenarios + diff), MFM real-RPC leakage, integration-contract drift vs live `br`/`bv`, audit-trail wiring through SettingsBridge.
- Determinism: build-scenarios.sh re-runs produce byte-identical output modulo `capturedAt` timestamps (acceptable per snapshot envelope contract; normalize via `jq 'del(..|.capturedAt?)'` for hash compare). 1 MEDIUM filed.
- MFM leakage: zero `fetch`/HTTP/socket calls in MockFlywheelMode/Client/LocalDemo*/replay/. Bead's "AuthBridge code path exercised" claim is half-true — IPC commands return mock tokens but bypass AuthBridge entirely. 1 MEDIUM filed.
- Contract drift fixed inline: `bv --robot-next` exists but my `bv.md` missed it; added to contract + snapshot.sh `capture::bv()`. Also documented "not in --robot-help but works" caveat for `--robot-recipes/diff/priority`.
- Audit wiring: zero refs to SettingsAuditTrail from SettingsBridge — every security-relevant setting writes to disk WITHOUT writing audit. 1 HIGH filed (3-line patch sketched).

## Round 1 — p4 (FuchsiaPond)
- Scope: TanStack routes, persisted shell store, ProjectRegistry, top-bar switcher accessibility, and hp-tg0/hp-j30 smoke coverage.
- Fixed: root launch now redirects to the persisted valid project/stage via `resolveShellLaunchTarget`, with removed-project fallback.
- Verification: `rch exec -- bun test apps/desktop/src/renderer/shell.test.tsx`, `rch exec -- bun run typecheck`, and UBS over changed renderer files are green.
- Filed: 5 MEDIUM follow-ups covering unknown project deep links, persistence hardening, registry storage failures, switcher focus/ARIA, and restart-state e2e coverage.
- No new CRITICAL findings in Round 1.

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

## SettingsBridge ↔ SettingsAuditTrail — [HIGH] audit module exists but is not wired
**Where:** `apps/desktop/src/main/SettingsBridge.ts` (any version) — zero references to `SettingsAuditTrail`/`auditResolvedTreeDelta`/`auditSettingsChange` across the entire main-process codebase. **Issue:** hp-wg5p's bead contract (#7 "Audit on change") declares that security-relevant setting changes (channel, telemetry, mock mode, push policy, safety preset, etc.) MUST emit `setting_changed` audit entries. The audit module enforces this when called. Today nobody calls it — every `setUserSettings`/`setProjectSettings` writes to disk silently. Audit log will be empty for these changes; the renderer "Audited" badge tells users what isn't actually happening. **Suggested fix:**
1. Add `auditSink?: SettingsAuditSink` and `actor?: SettingsActor` constructor options to SettingsBridge.
2. In `recompileAndBroadcast`, capture the pre-recompile resolved snapshot, compute the post-recompile snapshot, then `await auditResolvedTreeDelta(this.auditSink, before, after, this.actor ?? defaultActor, tier)`.
3. Default sink: append-only JSONL at `<paths.userFile>/../audit.jsonl` (same dir, separate file).
4. Test: change `desktop.updateChannel` → assert one audit entry written; change `daemon.logLevel` (not in security set) → assert no audit entry. **Reviewer:** p1   **Round:** 1

## bv integration contract — [HIGH] missing `bv.robot.next` capability + four "advertised vs working" caveats (FIXED INLINE)
**Where:** `docs/integration-contracts/bv.md` (capability table) + `scripts/research-spike/snapshot.sh:capture::bv()`   **Issue:** Live `bv --robot-help` advertises only 4 core commands (triage / next / plan / insights). My contract documented triage/plan/insights but MISSED `--robot-next` (single top recommendation; returns `{id, title, score, reasons[], unblocks, claim_command, show_command}`). Conversely my contract documented `--robot-recipes/diff/priority` which DO work but are NOT advertised by `--robot-help`. **Suggested fix:** Added `bv.robot.next` to bv.md capability table + snapshot.sh capture; flagged the three "works but not in help" surfaces with explicit notes. Done inline.   **Reviewer:** p1   **Round:** 1

## fixture-corpus seeder — [MEDIUM] capturedAt timestamps drift across re-runs
**Where:** `scripts/snapshot/seed/build-scenarios.sh` + `build-golden-outputs.sh`   **Issue:** Re-running the seeder produces byte-identical output EXCEPT for the `capturedAt` timestamp written into every meta.json + expected-outcome.json (see Round 1 determinism check). Acceptable for the v1 fixture corpus (the snapshot envelope contract specifies normalize-on-compare via `jq 'del(..|.capturedAt?)'`), but creates noise in `git diff` whenever a coordinator regenerates fixtures and breaks downstream hash-based fixture-version pinning.   **Suggested fix:** Accept `--captured-at <iso>` flag on both seeders; default to the SNAPSHOT's `meta.capturedAt` when present (matches the source data's deterministic timestamp); fall back to `now()` only when there's no source snapshot.   **Reviewer:** p1   **Round:** 1

## MockFlywheel auth dance — [MEDIUM] AuthBridge code path is NOT actually exercised end-to-end
**Where:** `apps/desktop/src/main/MockFlywheelMode.ts:130` (header comment), `apps/desktop/src/main/LocalDemoBootstrap.ts:12,87`, `apps/desktop/src/main/MockFlywheelClient.ts` (auth IPC commands)   **Issue:** The bead spec for hp-o74 says "Mock Flywheel must NOT replace daemon authentication — a mock daemon still issues mock pairing tokens / mock bearers so the AuthBridge code path is exercised." Today the IPC commands `mock-flywheel.auth.exchangePairing` and `mock-flywheel.auth.issueWsToken` return the mock tokens directly from the MockDaemonClient — they never call `AuthBridge.exchangePairingForBearer()` or `AuthBridge.issueWsToken()`. The auth code path is documented but not exercised. **Suggested fix:** When Phase 2 daemon binary lands, the MockFlywheelMode auth IPC commands should construct an in-process AuthBridge instance pointing at a mock daemon HTTP server (the daemon binary itself can run in `--mock-flywheel` mode with `--http-port 0` to bind locally) and route through it. For v1.0 (no daemon binary), the misleading comment should be softened to "AuthBridge token shape is honored; full HTTP round-trip exercised post-Phase-2".   **Reviewer:** p1   **Round:** 1

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

## Desktop shell — [HIGH] root launch did not restore the last active project/stage
**Where:** `apps/desktop/src/renderer/routes.tsx:20`   **Issue:** Before the Round 1 fix, the root route always rendered `ProjectPickerRoute`; persisted `lastProjectId` and per-project `lastStageId` only affected the card target after a user clicked a project, so app relaunch from `/` did not restore the last working context.   **Suggested fix:** Fixed inline by adding `resolveShellLaunchTarget()` in `apps/desktop/src/renderer/store.ts:219`, redirecting `/` to the persisted valid project/stage, and falling back to the most recently activated project when the persisted id was removed. Added focused unit coverage.   **Reviewer:** p4   **Round:** 1

## Desktop shell — [MEDIUM] arbitrary deep-link project ids are persisted as active projects
**Where:** `apps/desktop/src/renderer/shell/routes.tsx:55`   **Issue:** `StageRoute` reads params with `strict: false`, falls back to `defaultProjectId`, and calls `rememberProject(projectId)` for every stage route; `rememberProject()` at `apps/desktop/src/renderer/store.ts:362` does not verify the id exists in `projects`. A typo or stale deep link such as `/missing/swarm` becomes the persisted active project even though the switcher cannot list it.   **Suggested fix:** Add a route guard or loader that validates `params.projectId` against the registry/store before rendering a stage; unknown ids should redirect to the project picker or a not-found route. Make `rememberProject()` return a missing result or no-op for unknown ids.   **Reviewer:** p4   **Round:** 1

## Persisted shell state — [MEDIUM] restartability state bypasses durable settings storage
**Where:** `apps/desktop/src/renderer/store.ts:461`   **Issue:** Core restartability fields (`lastProjectId`, `activeProjectId`, per-project drawers/scroll/activity state) are persisted only through renderer `window.localStorage`. That path has no schema migration, disk-full handling, corruption recovery, or atomic write semantics, while Phase 1 already has a main-process `SettingsBridge` path intended for durable desktop settings.   **Suggested fix:** Replace the renderer-only persistence adapter with a small IPC-backed storage adapter over the SettingsBridge/client-settings file, keep a versioned schema, catch parse/write failures, preserve the in-memory state when writes fail, and add tests for corrupted JSON and quota/write exceptions.   **Reviewer:** p4   **Round:** 1

## ProjectRegistry — [MEDIUM] storage read/write failures are not contained
**Where:** `apps/desktop/src/main/ProjectRegistry.ts:63`   **Issue:** Registry construction calls `input.storage.read()` directly and `persist()` calls `this.storage.write()` at `apps/desktop/src/main/ProjectRegistry.ts:166` without a guard. Corrupt storage, permission errors, or disk-full writes can throw through construction or activation instead of falling back with a diagnostic.   **Suggested fix:** Wrap reads with a fallback to defaults plus an audit/logger event, wrap writes so the in-memory registry remains usable when persistence fails, and add unit tests for throwing `read()` and `write()` implementations.   **Reviewer:** p4   **Round:** 1

## Top-bar project switcher — [MEDIUM] focus and active-project semantics are incomplete
**Where:** `apps/desktop/src/renderer/topbar/ProjectSwitcher.tsx:168`   **Issue:** The switcher is keyboard-operable, but the popup has no `aria-activedescendant`/listbox semantics, active rows use only `data-active`, and the CSS has no `:focus-visible` rules for `.hh-project-switcher-button`, `.hh-project-row-main`, `.hh-icon-button`, or `.hh-text-button`; the search input explicitly removes its outline at `apps/desktop/src/renderer/styles.css:239`. Keyboard users can lose visible focus and screen readers do not get a reliable "current project" announcement inside the dialog.   **Suggested fix:** Model the popup as a combobox/listbox or menu with stable option ids, expose the highlighted option with `aria-activedescendant`, set `aria-current` or `aria-selected` on the active project, and add a shared `:focus-visible` token style for switcher buttons, row buttons, icon buttons, and the search input container.   **Reviewer:** p4   **Round:** 1

## Desktop smoke tests — [MEDIUM] project switch and restart restore are not covered end-to-end
**Where:** `apps/desktop/tests/smoke/e2e/desktop-shell.spec.ts:4`   **Issue:** The smoke path covers boot, sidebar navigation, Activity panel, and a skipped command-palette assertion; hp-j30's e2e at `apps/desktop/tests/e2e/hp-j30-desktop-shell.spec.ts:19` follows the same single-project path. Neither test opens the project switcher, switches projects, persists state, reloads/restarts, or asserts the last project/stage/activity state is restored.   **Suggested fix:** Add a Playwright test that boots, switches to `mock-flywheel-project`, navigates to a non-default stage, toggles Activity state, reloads or restarts the app context, and asserts `/` restores the selected project/stage with structured JSON-line logging.   **Reviewer:** p4   **Round:** 1
