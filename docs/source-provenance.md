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
| Verbatim notice    | Top of every file under `apps/desktop/src/vendored/t3code/**`   |
| Attribution rules  | See `plan.md` Appendix B "License attribution" + project NOTICE |

### Vendored layout

Lifted files land under `apps/desktop/src/vendored/t3code/` verbatim, with
the MIT notice preserved at the top of each file. Adaptations (rebranding,
rewiring, schema swaps) happen in Hoopoe's own files that import from
`vendored/`. Vendored files are not edited in place beyond mechanical mass
renames (e.g., `T3CODE_*` env keys → `HOOPOE_*`, branding strings).

### File inventory

The authoritative file inventory lives in `plan.md` Appendix B. Files are
lifted incrementally across Phase 1 beads:

- **hp-xru** (this bead) — clone, pin SHA, scaffold monorepo skeleton, copy
  the verbatim `LICENSE` into `apps/desktop/src/vendored/t3code/LICENSE`.
- **hp-15s** — vendor desktop lifecycle files (`clientPersistence.ts`,
  `backendPort.ts`, `backendReadiness.ts`, `serverListeningDetector.ts`,
  `desktopSettings.ts`, `updateMachine.ts`, `updateChannels.ts`,
  `updateState.ts`, `runtimeArch.ts`, `syncShellEnvironment.ts`,
  `windowReveal.ts`, `confirmDialog.ts`, `appBranding.ts`).
- **hp-191** — vendor build pipeline (`scripts/build-desktop-artifact.ts`,
  `scripts/mock-update-server.ts`, `scripts/release-smoke.ts`,
  `.github/workflows/release.yml`).
- **hp-rth** — vendor keybindings + AST + last-rule-wins; add command
  registry layer.
- **hp-4bt** — vendor three-store settings system + atomic-write + PubSub.
- **hp-zir** — decompose the 2,175-line `apps/desktop/src/main.ts` into
  `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`,
  `SettingsBridge`, `AuthBridge` modules under `apps/desktop/src/main/`.

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
