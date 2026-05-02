# `@hoopoe/desktop`

Electron + TypeScript + React desktop app — the cockpit UI the user sees.

## Status

Pre-Phase-1 scaffold (hp-xru). The Electron lifecycle, auth bridge, settings
system, keybindings, and build pipeline are lifted from t3code in subsequent
beads (see `docs/source-provenance.md`):

- **hp-xru** (this scaffold) — workspace, `tsconfig`, smoke test only.
- **hp-15s** — vendor desktop lifecycle files into `src/vendored/t3code/`.
- **hp-rth** — vendor keybindings + AST + command registry.
- **hp-4bt** — vendor three-store settings + atomic-write + PubSub.
- **hp-zir** — decompose t3code's 2,175-line `main.ts` into
  `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`,
  `SettingsBridge`, `AuthBridge` under `src/main/`.
- **hp-spx** — macOS Keychain via Electron `safeStorage`.

## Vendored t3code

`src/vendored/t3code/` holds files lifted verbatim from
[`github.com/pingdotgg/t3code`](https://github.com/pingdotgg/t3code) (MIT,
Copyright 2026 T3 Tools Inc.). The MIT notice is preserved at the top of
every vendored file and the verbatim `LICENSE` lives at
`src/vendored/t3code/LICENSE`. **Do not edit `vendored/` files in place**
beyond mechanical mass renames (e.g., `T3CODE_*` → `HOOPOE_*`); adaptations
live in our own files that import from `vendored/`. See `plan.md` Appendix B
for the file inventory and editing rules.

## Tech stack (per `plan.md §3`)

- Electron + TypeScript + React + Vite + Tailwind (custom design tokens)
- TanStack Router (typed stage routes), TanStack Query (server cache),
  Zustand (ephemeral UI state)
- CodeMirror 6 (plan editor), xterm.js (Diagnostics "Show raw pane" debug
  toggle only — `Guardrail 12`), React Flow (DAG)
- macOS Keychain via Electron `safeStorage`
- Local cache in SQLite or IndexedDB (read-only mirror, never canonical —
  `Guardrail 4`)
- **Effect framework is NOT adopted.** Patterns are lifted into plain
  TypeScript at lower cognitive cost (`plan.md §3`).
