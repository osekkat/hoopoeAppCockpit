# Vendored from t3code (MIT)

Most implementation files in this directory are copied or adapted from
[`github.com/pingdotgg/t3code`](https://github.com/pingdotgg/t3code) under
the MIT License, Copyright (c) 2026 T3 Tools Inc. A small set of
Hoopoe-owned support files also lives here when it exists only to compile,
export, document, or test those lifted helpers.

The pinned upstream SHA is recorded in `docs/source-provenance.md` at the
project root. The verbatim MIT license text lives next to this README at
`./LICENSE`.

## Editing rules (Appendix B of `plan.md`)

1. **Do not edit copied/adapted upstream files in this directory in place**
   beyond mechanical mass renames (e.g., `T3CODE_*` env keys → `HOOPOE_*`,
   branding strings).
2. Every copied/adapted t3code implementation file must carry the t3code MIT
   notice at the top:

   ```text
   // Originally from github.com/pingdotgg/t3code (MIT License)
   // Copyright (c) 2026 T3 Tools Inc.
   // Adapted for Hoopoe.
   //
   // Full MIT license text: vendored/t3code/LICENSE
   ```
3. Adaptations (rebranding beyond mass-rename, schema swaps, daemon-launch
   wiring, monolith decomposition) live in Hoopoe's own files under
   `apps/desktop/src/main/`, `apps/desktop/src/renderer/`, etc., that
   `import` from `vendored/`. The diff against upstream stays small enough
   to re-merge later if needed.
4. Files we explicitly do **NOT** lift: `apps/server/`, `apps/web/`,
   `apps/marketing/`, `packages/effect-acp/`,
   `packages/effect-codex-app-server/`, `packages/contracts/`, the Effect
   framework wholesale.

## Hoopoe-owned exceptions

These files are not upstream t3code source and therefore do not carry the
t3code source notice. They must stay clearly marked as Hoopoe-owned:

| File pattern                  | Purpose |
| ----------------------------- | ------- |
| `_shims.ts`                   | Plain-TypeScript shim layer for lifted modules |
| `settings/index.ts`           | Barrel re-export for lifted settings helpers |
| `*.test.ts`                   | Hoopoe-authored regression tests for lifted helpers |
| `keybindings/*.test.ts`       | Hoopoe-authored keybinding parser/evaluator tests |
| `README.md`                   | Hoopoe-authored provenance and editing guide |

## What lands here, when

- **hp-xru** (this bead) — verbatim `LICENSE` only.
- **hp-15s** — desktop lifecycle (`clientPersistence.ts`, `backendPort.ts`,
  `backendReadiness.ts`, `serverListeningDetector.ts`, `desktopSettings.ts`,
  `updateMachine.ts`, `updateChannels.ts`, `updateState.ts`,
  `runtimeArch.ts`, `syncShellEnvironment.ts`, `windowReveal.ts`,
  `confirmDialog.ts`, `appBranding.ts`).
- **hp-191** — build-pipeline lift landed outside this directory:
  `scripts/build-desktop-artifact.ts`, `scripts/mock-update-server.ts`, and
  `.github/workflows/release.yml`.
- **hp-rth** — keybindings AST + last-rule-wins source.
- **hp-4bt** — three-store settings + atomic-write + PubSub source.
