# Vendored from t3code (MIT)

Files in this directory are lifted verbatim from
[`github.com/pingdotgg/t3code`](https://github.com/pingdotgg/t3code) under
the MIT License, Copyright (c) 2026 T3 Tools Inc.

The pinned upstream SHA is recorded in `docs/source-provenance.md` at the
project root. The verbatim MIT license text lives next to this README at
`./LICENSE`.

## Editing rules (Appendix B of `plan.md`)

1. **Do not edit files in this directory in place** beyond mechanical mass
   renames (e.g., `T3CODE_*` env keys → `HOOPOE_*`, branding strings).
2. Every lifted file must carry the t3code MIT notice at the top:

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

## What lands here, when

- **hp-xru** (this bead) — verbatim `LICENSE` only.
- **hp-15s** — desktop lifecycle (`clientPersistence.ts`, `backendPort.ts`,
  `backendReadiness.ts`, `serverListeningDetector.ts`, `desktopSettings.ts`,
  `updateMachine.ts`, `updateChannels.ts`, `updateState.ts`,
  `runtimeArch.ts`, `syncShellEnvironment.ts`, `windowReveal.ts`,
  `confirmDialog.ts`, `appBranding.ts`).
- **hp-191** — build-pipeline scripts under
  `apps/desktop/scripts/vendored/t3code/` and project root
  `scripts/vendored/t3code/`.
- **hp-rth** — keybindings AST + last-rule-wins source.
- **hp-4bt** — three-store settings + atomic-write + PubSub source.
