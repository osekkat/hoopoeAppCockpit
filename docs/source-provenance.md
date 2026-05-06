# Source provenance

Lifted external sources used in the Hoopoe codebase, with pinned commit SHAs
and attribution rules. Every entry here corresponds to a `THIRD-PARTY
ATTRIBUTIONS` section in the project root `NOTICE` file.

When the upstream ships fixes we want, we cherry-pick deliberately rather
than merging automatically (per `plan.md §14` "Upstream t3code drift" risk).
Every quarter we review the upstream `CHANGELOG` and decide what to bring in;
the new pinned SHA is recorded here.

## t3code

| Field              | Value                                                           |
| ------------------ | --------------------------------------------------------------- |
| Tool               | t3code (T3 Code Alpha)                                          |
| Repo               | https://github.com/pingdotgg/t3code                             |
| License            | MIT                                                             |
| Copyright          | Copyright (c) 2026 T3 Tools Inc.                                |
| Pinned commit SHA  | `460d9c3eb977bc0341876527af7fde591133925f`                      |
| Pinned commit date | 2026-05-02 (UTC-7)                                              |
| Pinned commit msg  | Refactor provider settings to declarative metadata (#2452)      |
| Lift date          | 2026-05-02                                                      |
| License file path  | `apps/desktop/src/vendored/t3code/LICENSE`                      |
| Verbatim notice    | Top of every copied/adapted t3code implementation file          |
| Attribution rules  | See `plan.md` Appendix B "License attribution" + project NOTICE |

### Vendored layout

Copied or adapted t3code implementation files land under
`apps/desktop/src/vendored/t3code/` with the MIT notice preserved at the top
of each copied/adapted file. Adaptations (rebranding, rewiring, schema swaps)
happen in Hoopoe's own files that import from `vendored/`. Vendored
implementation files are not edited in place beyond mechanical mass renames
(e.g., `T3CODE_*` env keys → `HOOPOE_*`, branding strings).

The same directory also contains a small set of Hoopoe-authored support files
that exist only to compile, export, document, or test the lifted helpers. These
files are not upstream t3code source, do not carry the upstream MIT source
notice, and must be clearly marked as Hoopoe-owned:

| Hoopoe-owned file pattern                                      | Purpose |
| -------------------------------------------------------------- | ------- |
| `apps/desktop/src/vendored/t3code/_shims.ts`                   | Plain-TypeScript shim layer for lifted modules |
| `apps/desktop/src/vendored/t3code/settings/index.ts`           | Barrel re-export for the lifted settings helpers |
| `apps/desktop/src/vendored/t3code/*.test.ts`                   | Hoopoe-authored regression tests for lifted helpers |
| `apps/desktop/src/vendored/t3code/keybindings/*.test.ts`       | Hoopoe-authored keybinding parser/evaluator tests |
| `apps/desktop/src/vendored/t3code/README.md`                   | Hoopoe-authored provenance and editing guide |
| `apps/desktop/src/main/*.ts` (`BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`, `SettingsBridge`, `AuthBridge`) | Six Hoopoe-owned integration seams that replace t3code's 2,175-line `apps/desktop/src/main.ts` monolith (Appendix B "Anti-patterns to refuse" #4). They import from the vendored t3code helpers but do not contain substantial copied source; each module begins with a `// Hoopoe-owned.` marker naming the helpers it composes. If a later edit pastes a substantial copied t3code function inline, the upstream MIT notice must be added at the top of that file. |

### File inventory

The authoritative file inventory lives in `plan.md` Appendix B. The current
on-disk vendored tree is exactly the copied/adapted files under
`apps/desktop/src/vendored/t3code/`, the verbatim `LICENSE`, plus the
Hoopoe-owned support files listed above. Files are lifted incrementally across
Phase 1 beads:

- **hp-xru** (this bead) — clone, pin SHA, scaffold monorepo skeleton, copy
  the verbatim `LICENSE` into `apps/desktop/src/vendored/t3code/LICENSE`.
- **hp-15s** — vendor desktop lifecycle files (`clientPersistence.ts`,
  `backendPort.ts`, `backendReadiness.ts`, `serverListeningDetector.ts`,
  `desktopSettings.ts`, `updateMachine.ts`, `updateChannels.ts`,
  `updateState.ts`, `runtimeArch.ts`, `syncShellEnvironment.ts`,
  `windowReveal.ts`, `confirmDialog.ts`, `appBranding.ts`).
- **hp-191** — vendor build pipeline files that exist on disk today:
  `scripts/build-desktop-artifact.ts`, `scripts/mock-update-server.ts`, and
  `.github/workflows/release.yml`. `scripts/release-smoke.ts` is intentionally
  not lifted; Hoopoe uses its Bun/Playwright smoke suites and
  `scripts/e2e/run-e2e.ts` instead. The electron-builder config is synthesized
  in `scripts/build-desktop-artifact.ts` from `apps/desktop/package.json`
  instead of stored in a separate `build.yml`.
- **hp-rth** — vendor keybindings + AST + last-rule-wins; add command
  registry layer. The vendored files are
  `apps/desktop/src/vendored/t3code/keybindings/{parser,evaluator,types}.ts`;
  matching `*.test.ts` files are Hoopoe-authored regression tests.
- **hp-4bt** — vendor three-store settings helpers. The lifted helper files
  are `settings/atomicWrite.ts` and `settings/stripDefaults.ts`; the
  `settings/index.ts` barrel is Hoopoe-owned support code.
- **hp-zir** — decompose the 2,175-line `apps/desktop/src/main.ts` into
  `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`,
  `SettingsBridge`, `AuthBridge` modules under `apps/desktop/src/main/`.

The t3code desktop script wrappers
`apps/desktop/scripts/{dev-electron,start-electron,smoke-test}.mjs` are not
lifted. Hoopoe uses the plain `vite`, `tsdown`, and `playwright` commands wired
through `apps/desktop/package.json` instead.

### Files we explicitly do NOT lift

Per `plan.md` Appendix B:

- `apps/server/` (entire t3code TypeScript server) — Hoopoe's daemon is Go.
- `apps/web/` — Hoopoe's renderer is purpose-built.
- `apps/marketing/` — irrelevant.
- `packages/effect-acp/` — Agent Client Protocol; Hoopoe wraps CLIs, not
  ACP-speaking agents.
- `packages/effect-codex-app-server/` — OpenAI-internal protocol.
- `packages/contracts/` — recreated in `packages/schemas/` from scratch as
  Go-readable OpenAPI, so the daemon and desktop never drift.
- The Effect framework wholesale — patterns adopted in plain TypeScript at
  lower cognitive cost (`plan.md §3`).

### Pinned-clone location

The reference clone produced during `hp-xru` lives at `/tmp/t3code-pinned`.
This is a local working clone for inspecting upstream as we lift; it is not
part of the Hoopoe repo and is not relied on at runtime.

## Codex-shape scrub (Phase 1 week 2 — `hp-4nrd`)

`plan.md §14` names "Lifted code carries Codex-shaped assumptions" as a
real risk. T3 code's desktop layer was written for a chat/agent product;
subtle assumptions around `Thread`, `Chat`, `Provider`, `MessageList`,
`Conversation` can leak through scrubbing into Hoopoe's purpose-built
code. The lint at
`scripts/codex-shape-scrub/check-codex-shape-scrub.ts` is the durable
enforcement (wired into root `bun run lint` and `bun run test`).

The one-time manual scrub of the lifted surface, run at the end of
Phase 1 week 2:

- [x] `grep '\bThread\b' apps/desktop/src/` (excluding vendored) →
      0 matches; bead/swarm/activity language used throughout the
      `apps/desktop/src/main/` modules and adapter files.
- [x] `grep '\bProvider\b' apps/desktop/src/` (excluding vendored) →
      0 matches; CAAM-account language is used where t3code would say
      "provider". (One match in `packages/fixtures/src/validate.ts` —
      the comment `Provider-secret patterns we forbid in the corpus`
      refers to the SDK secrets we ban under Guardrail #11; not a
      Codex-shape identifier.)
- [x] `grep '\bChat\b' apps/desktop/src/` (excluding vendored) →
      0 matches as a bare PascalCase identifier. The
      `orchestrator-chat` tending agent is referenced as a string
      / kebab-case literal only.
- [x] `grep 'MAX_' apps/desktop/src/` (excluding vendored) →
      0 silent caps; bounded-queue patterns in `SettingsBridge.ts`
      use `MAX_PENDING_PER_SUBSCRIBER` with an explicit
      `dropped: N` notice (Appendix B anti-pattern #3 closed).
- [x] `grep -E '(Conversation|ChatTurn|MessageList)'`
      `apps/desktop/src/` (excluding vendored) → 0 matches.
- [x] **Import audit:** every `import from '../vendored/t3code/...'`
      under `apps/desktop/src/main/` resolves to one of the
      Appendix B "Patterns lifted" approved files
      (`atomicWrite`, `stripDefaults`, `keybindings/{parser,evaluator,types}`,
      `windowReveal`, `clientPersistence`, `desktopSettings`,
      `runtimeArch`, `serverListeningDetector`, `backendPort`,
      `backendReadiness`, `updateMachine`, `updateChannels`,
      `updateState`, `appBranding`, `confirmDialog`,
      `syncShellEnvironment`, `_shims`).
- [x] **Storybook stories** — design-system stories use Hoopoe domain
      language (`bead`, `swarm`, `agent`, `plan`, `activity`) per
      hp-i62 components. No `chat` / `thread` / `conversation`
      stories.
- [x] **Component names** — every primitive in
      `packages/design-system/src/components/` matches the design
      system list (`StageHeader`, `StatusPill`, `PriorityChip`,
      `BeadCard`, `AgentTile`, `CoverageBar`, `TerminalPane`,
      `TimelineRow`, `HealthKpiCard`, `ApprovalDialog`,
      `CommandPalette`). No t3code-shaped names.

Signed: GreenBear · 2026-05-02. The lint
(`scripts/codex-shape-scrub/check-codex-shape-scrub.ts`) gates every
future PR; future reviews can reference this checklist for the
one-time pass.
