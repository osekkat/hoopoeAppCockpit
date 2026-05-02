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
