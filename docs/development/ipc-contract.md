# IPC Contract (hp-n5za)

The Hoopoe desktop app's renderer is sandboxed (`contextIsolation: true`, `sandbox: true`, `nodeIntegration: false` per hp-rflj). The ONLY way the renderer reaches main-process privileges is `window.hoopoe`, exposed by `apps/desktop/electron/preload.ts`. Every method on that object routes through `ipcRenderer.invoke` to a typed channel registered with `IpcRegistry`.

Pre-hp-n5za, the renderer could pass any string into `daemon.request(method, body)` and `daemon.subscribe(topic, listener)`; the preload happily forwarded both to main, which forwarded to the daemon. Three HIGH findings filed during review (`review-findings.md`):

- "Renderer preload — [HIGH] no allowlist gate on daemon.request"
- "Renderer preload — [HIGH] no allowlist gate on daemon.subscribe"
- "IPC — [HIGH] dispatch type-safety is illusory pending hp-r3i schemas"

This bead closes the first two and adds defense-in-depth around the third.

## Single source of truth

`packages/schemas/preload-api.yaml` is the **authoritative** declaration of every method, topic, channel, and direct-channel input/output type-name pair the renderer can drive. The codegen at `packages/schemas/scripts/gen-preload-contract.ts` emits `apps/desktop/src/shared/ipc-contract.gen.ts` from that YAML; the manual `apps/desktop/src/shared/ipc-contract.ts` re-declares the same constants AND owns the runtime guard helpers, error class, and TypeScript shape interfaces that don't yet have a generator. Both files MUST agree — the parity test below fails the build if they drift.

The four families of identifiers exposed from each file:

- `DAEMON_REQUEST_METHODS` — daemon RPC methods (`ping`, `auth.exchangePairingForBearer`, `settings.get`, `projects.list`, `approvals.approve`, …). Multiplexed over `hoopoe.daemon.request`.
- `DAEMON_SUBSCRIBE_TOPICS` — WS event topics the renderer is entitled to observe (`events.swarm`, `events.beads`, …). **Internal-only topics (`events.audit`, `events.redaction`, `events.settings.tier-merge`) are intentionally absent** — the renderer cannot subscribe to those. Multiplexed over `hoopoe.daemon.subscribe` per-subscription channel suffixes.
- `PRELOAD_IPC_CHANNELS` — direct preload-layer channel names (one per non-daemon-RPC preload method: `hoopoe.settings.get`, `hoopoe.power.acquire`, `hoopoe.clone.discard-local-changes`, …).
- `PRELOAD_IPC_CHANNEL_CONTRACTS` — for every INVOKE-style entry in `PRELOAD_IPC_CHANNELS`, a record of `{ channel, input, output }` type-name pairs that pin the input/output validators registered with IpcRegistry. The codegen gate in `gen-preload-contract.ts:assertValidPreloadContracts` enumerates the multiplexer + watch channels that opt out of this requirement.
- `INTERNAL_IPC_COMMAND_PREFIXES` — namespace prefixes (`mock-flywheel.`, `internal.`) under which the IpcRegistry accepts main-process-internal commands. The renderer cannot reach these because they aren't channels — they're command IDs registered by main-side modules (e.g., MockFlywheelClient).

Three layers consume these constants:

- the preload (`apps/desktop/electron/preload.ts`) imports `PRELOAD_IPC_CHANNELS` + the runtime guards from the manual file;
- `IpcRegistry` (`apps/desktop/src/main/IpcRegistry.ts`) imports `isAllowedRegistryCommandId` + `isPreloadIpcChannel` from the same;
- the codegen-validator (`packages/schemas/scripts/validate-preload-codegen.ts`) re-runs the generator against the YAML and asserts the on-disk `ipc-contract.gen.ts` matches.

Three runtime parity tests fail the build if anything drifts:

- `apps/desktop/src/shared/ipc-contract.test.ts:110-119` — manual `ipc-contract.ts` constants must `.toEqual(...)` the generated `ipc-contract.gen.ts` constants (DAEMON_REQUEST_METHODS, DAEMON_SUBSCRIBE_TOPICS, PRELOAD_IPC_CHANNELS, PRELOAD_IPC_CHANNEL_CONTRACTS, MOCK_FLYWHEEL_COMMANDS, INTERNAL_IPC_COMMANDS).
- `apps/desktop/electron/preload.contract.test.ts` — every channel registrable on `IpcRegistry` matches the allowlist; non-allowlisted ids are refused.
- `bun run --cwd packages/schemas validate` — re-runs the YAML codegen and diffs against `ipc-contract.gen.ts` on disk; surfaces a `*.drift` artifact in the `schemas-codegen-drift.yml` workflow.

## Enforcement layers

| Layer | When | What it refuses | Error |
| --- | --- | --- | --- |
| TypeScript types | `bun run typecheck` | `daemon.request("evil", body)` — `evil` is not assignable to `DaemonRequestMethod`. | TS2345 (compile-time) |
| Preload runtime | `window.hoopoe.daemon.request("evil", body)` from a non-TS or third-party renderer | Unknown method/topic before any IPC fires | `IpcContractError` (rejected Promise / thrown sync) |
| IpcRegistry register | `register({ id: "evil", ... })` | Command IDs outside the allowlist | `IpcContractError({ kind: "channel" })` |
| IpcRegistry dispatch | `dispatch("evil", input)` | Same — defense-in-depth in case `register()` is bypassed | `IpcContractError({ kind: "channel" })` |

The error class is `IpcContractError` from `src/shared/ipc-contract.ts`. It carries `{ kind: "method" \| "topic" \| "channel", attempted: string }` so renderer error UI can distinguish "this is a contract bug, file an issue" from "daemon unreachable".

## Adding a new daemon method

1. Edit `packages/schemas/preload-api.yaml`. Append the kebab/dot-canonical method name (matching the daemon's HTTP/gRPC route exactly) under `daemonRequestMethods`.
2. Re-run codegen: `bun run --cwd packages/schemas generate` (or `bun run --cwd packages/schemas validate` to confirm the existing `ipc-contract.gen.ts` already matches your edit). The script emits `apps/desktop/src/shared/ipc-contract.gen.ts` from the YAML.
3. Mirror the addition in `apps/desktop/src/shared/ipc-contract.ts`. Append to `DAEMON_REQUEST_METHODS` so the manual security-boundary file stays in lockstep with the generated file. Add the TypeScript input/output shape interfaces in the same file (the manual side still owns the shape interfaces; the generator does not yet emit them).
4. Run `bun run --cwd apps/desktop test` — `ipc-contract.test.ts` checks the array for duplicates AND parity with the generated file (lines 110-119); `preload.contract.test.ts` checks the IpcRegistry side picks the new id up.
5. Wire a main-side handler via `IpcRegistry.register({ id: "hoopoe.daemon.request", ... })` that branches on `body.method` and calls into the daemon client.
6. If the new method is **security-relevant**, add it to `SECURITY_RELEVANT_SETTING_KEYS` in `SettingsAuditTrail.ts` — every call records a `setting_changed` audit entry (hp-6obn).

## Adding a new subscription topic

Same flow as a daemon method, but the YAML edit goes under `daemonSubscribeTopics` and the manual mirror goes into `DAEMON_SUBSCRIBE_TOPICS`. Be doubly careful: subscription topics let the renderer READ data. If the data is internal-only (audit, redaction, tier-merge), DO NOT add it. The threat model assumes any string the renderer constructs becomes observable.

## Adding a new direct preload channel

Direct preload channels (e.g., `hoopoe.settings.get`, `hoopoe.power.acquire`) bypass the daemon multiplexer and reach a dedicated IpcRegistry handler in main.

1. Edit `packages/schemas/preload-api.yaml`. Append a `<key>: hoopoe.<namespace>.<verb>` entry under `preloadChannels`. INVOKE-style channels MUST also declare an entry under the typed-contract section pointing at TypeScript interface names (the codegen gate refuses any new channel that opts out without an explicit allowlist entry — only the multiplexers and `settingsWatch` opt out).
2. Re-run codegen + validate as above.
3. Mirror the addition in `apps/desktop/src/shared/ipc-contract.ts` (`PRELOAD_IPC_CHANNELS` + `PRELOAD_IPC_CHANNEL_CONTRACTS`) and add the TypeScript input/output shape interfaces.
4. Add the bridge method to `apps/desktop/electron/preload.ts` so the renderer can reach it via `window.hoopoe.<namespace>.<verb>(...)`.
5. Register a main-side handler via `IpcRegistry.register({ id: PRELOAD_IPC_CHANNELS.<key>, validateInput, validateOutput, handler })`. The registry refuses any preload channel registered without both validators (`MissingIpcValidatorError`).
6. Add tests covering the validator + handler. The `preload.contract.test.ts` parity check picks up the new id automatically.

## Adding a main-process-internal command (no renderer access)

Use the `internal.` prefix. Example: `internal.health-snapshot.refresh`. The IpcRegistry allows it; preload has no path to invoke it (the preload only exposes the channel methods). Tests in `IpcRegistry.test.ts` use `internal.*` IDs.

## Mock Flywheel commands

Live under `mock-flywheel.*` (registered by `MockFlywheelClient.registerMockFlywheelClient`). The prefix is in `INTERNAL_IPC_COMMAND_PREFIXES`. The renderer cannot reach these directly — they are dispatched by main when mock mode is active.

## Manual + generated: the current hybrid state

`packages/schemas/preload-api.yaml` is authoritative; `apps/desktop/src/shared/ipc-contract.gen.ts` is its regenerable artifact; `apps/desktop/src/shared/ipc-contract.ts` is a hand-maintained mirror that ALSO carries the runtime-guard helpers, the `IpcContractError` class, and the TypeScript input/output shape interfaces (the YAML codegen does not emit these today). The parity tests at `ipc-contract.test.ts:110-119` keep the two const sets in lockstep.

Future direction: when the codegen learns to emit the shape interfaces and the runtime guards consume only generated unions, the manual file collapses to a thin shim around the helpers and error class. The parity tests stay; only the source of the constants moves. Both files remain `import`-able from the same path so consumers don't move with the migration.

A failed `bun run --cwd packages/schemas validate` (or the `schemas-codegen-drift.yml` CI workflow) blocks any PR that drifts the YAML from the generated file. A failed `apps/desktop/src/shared/ipc-contract.test.ts` blocks any PR that drifts the manual mirror.

## Threat model assumptions

- The renderer is **untrusted** from the main process's perspective. Any string it constructs can be a buffer overflow attempt, a path-traversal probe, or a fishing expedition for internal topics.
- Preload bytes are LOADED into the renderer process at startup, so they share the renderer's memory space. A successful renderer compromise that escalates to the preload layer COULD theoretically rewrite the helpers — but at that point the attacker controls the renderer entirely and would just call `ipcRenderer.invoke` directly. The IpcRegistry's main-side allowlist enforcement is the only durable boundary.
- Adding to the allowlist is the kind of edit a security review should always look at. Keep `DAEMON_REQUEST_METHODS` and `DAEMON_SUBSCRIBE_TOPICS` short.
