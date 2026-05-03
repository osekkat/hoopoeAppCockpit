# IPC Contract (hp-n5za)

The Hoopoe desktop app's renderer is sandboxed (`contextIsolation: true`, `sandbox: true`, `nodeIntegration: false` per hp-rflj). The ONLY way the renderer reaches main-process privileges is `window.hoopoe`, exposed by `apps/desktop/electron/preload.ts`. Every method on that object routes through `ipcRenderer.invoke` to a typed channel registered with `IpcRegistry`.

Pre-hp-n5za, the renderer could pass any string into `daemon.request(method, body)` and `daemon.subscribe(topic, listener)`; the preload happily forwarded both to main, which forwarded to the daemon. Three HIGH findings filed during review (`review-findings.md`):

- "Renderer preload — [HIGH] no allowlist gate on daemon.request"
- "Renderer preload — [HIGH] no allowlist gate on daemon.subscribe"
- "IPC — [HIGH] dispatch type-safety is illusory pending hp-r3i schemas"

This bead closes the first two and adds defense-in-depth around the third.

## Single source of truth

`apps/desktop/src/shared/ipc-contract.ts` declares — as `as const` arrays — every method and topic the renderer can drive:

- `DAEMON_REQUEST_METHODS` — daemon RPC methods (`ping`, `auth.exchangePairingForBearer`, `settings.get`, `projects.list`, `approvals.approve`, …).
- `DAEMON_SUBSCRIBE_TOPICS` — WS event topics the renderer is entitled to observe (`events.swarm`, `events.beads`, …). **Internal-only topics (`events.audit`, `events.redaction`, `events.settings.tier-merge`) are intentionally absent** — the renderer cannot subscribe to those.
- `PRELOAD_IPC_CHANNELS` — preload-layer channel names (one per direct preload method, NOT the daemon-RPC methods which all multiplex over `hoopoe.daemon.request`).
- `INTERNAL_IPC_COMMAND_PREFIXES` — namespace prefixes (`mock-flywheel.`, `internal.`) under which the IpcRegistry accepts main-process-internal commands. The renderer cannot reach these because they aren't channels — they're command IDs registered by main-side modules (e.g., MockFlywheelClient).

Both the preload (`apps/desktop/electron/preload.ts`) and `IpcRegistry` consume this file. A runtime parity test (`apps/desktop/src/shared/ipc-contract.test.ts` + `apps/desktop/electron/preload.contract.test.ts`) fails the build if they ever drift.

## Enforcement layers

| Layer | When | What it refuses | Error |
| --- | --- | --- | --- |
| TypeScript types | `bun run typecheck` | `daemon.request("evil", body)` — `evil` is not assignable to `DaemonRequestMethod`. | TS2345 (compile-time) |
| Preload runtime | `window.hoopoe.daemon.request("evil", body)` from a non-TS or third-party renderer | Unknown method/topic before any IPC fires | `IpcContractError` (rejected Promise / thrown sync) |
| IpcRegistry register | `register({ id: "evil", ... })` | Command IDs outside the allowlist | `IpcContractError({ kind: "channel" })` |
| IpcRegistry dispatch | `dispatch("evil", input)` | Same — defense-in-depth in case `register()` is bypassed | `IpcContractError({ kind: "channel" })` |

The error class is `IpcContractError` from `src/shared/ipc-contract.ts`. It carries `{ kind: "method" \| "topic" \| "channel", attempted: string }` so renderer error UI can distinguish "this is a contract bug, file an issue" from "daemon unreachable".

## Adding a new daemon method

1. Edit `apps/desktop/src/shared/ipc-contract.ts`. Append to `DAEMON_REQUEST_METHODS`. The kebab/dot-canonical name should match the daemon's HTTP/gRPC route exactly.
2. Add a TypeScript shape for input + output (Phase 2.5 hp-r3i auto-generates these from `packages/schemas/preload-api.yaml`; until then, write them by hand and put them next to the const).
3. Run `bun run --cwd apps/desktop test` — `ipc-contract.test.ts` checks the array for duplicates / shape; `preload.contract.test.ts` checks the IpcRegistry side picks the new id up.
4. Wire a main-side handler via `IpcRegistry.register({ id: "hoopoe.daemon.request", ... })` that branches on `body.method` and calls into the daemon client.
5. If the new method is **security-relevant**, add it to `SECURITY_RELEVANT_SETTING_KEYS` in `SettingsAuditTrail.ts` — every call records a `setting_changed` audit entry (hp-6obn).

## Adding a new subscription topic

Same flow. Be doubly careful: subscription topics let the renderer READ data. If the data is internal-only (audit, redaction, tier-merge), DO NOT add it. The threat model assumes any string the renderer constructs becomes observable.

## Adding a main-process-internal command (no renderer access)

Use the `internal.` prefix. Example: `internal.health-snapshot.refresh`. The IpcRegistry allows it; preload has no path to invoke it (the preload only exposes the channel methods). Tests in `IpcRegistry.test.ts` use `internal.*` IDs.

## Mock Flywheel commands

Live under `mock-flywheel.*` (registered by `MockFlywheelClient.registerMockFlywheelClient`). The prefix is in `INTERNAL_IPC_COMMAND_PREFIXES`. The renderer cannot reach these directly — they are dispatched by main when mock mode is active.

## Relationship to hp-r3i (Phase 2.5)

When `packages/schemas/preload-api.yaml` lands and the codegen pipeline runs, the literal arrays in `ipc-contract.ts` are replaced with generated unions and the shape interfaces become generated `import` types. The runtime guards stay as-is and just consume the generated set. The parity tests stay as-is and verify generated == actually-registered. Until then, this manual-const file is the security boundary.

## Threat model assumptions

- The renderer is **untrusted** from the main process's perspective. Any string it constructs can be a buffer overflow attempt, a path-traversal probe, or a fishing expedition for internal topics.
- Preload bytes are LOADED into the renderer process at startup, so they share the renderer's memory space. A successful renderer compromise that escalates to the preload layer COULD theoretically rewrite the helpers — but at that point the attacker controls the renderer entirely and would just call `ipcRenderer.invoke` directly. The IpcRegistry's main-side allowlist enforcement is the only durable boundary.
- Adding to the allowlist is the kind of edit a security review should always look at. Keep `DAEMON_REQUEST_METHODS` and `DAEMON_SUBSCRIBE_TOPICS` short.
