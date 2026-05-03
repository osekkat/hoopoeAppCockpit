# Security

`plan.md` remains authoritative. This file records implementation guidance that
daemon and desktop code should follow when the plan names a security boundary.

## Renderer Hardening (hp-rflj, plan.md §5.4 lines 1-2)

Hoopoe's renderer process cannot reach the filesystem, network, SSH,
Keychain, or daemon tokens directly. Everything goes through a small typed
preload API + main-process IPC handlers that validate schemas and user
intent.

### `BrowserWindow.webPreferences` (`apps/desktop/src/main/window-policy.ts::SAFE_WEB_PREFERENCES`)

```ts
{
  contextIsolation: true,
  sandbox: true,
  nodeIntegration: false,
  nodeIntegrationInWorker: false,
  nodeIntegrationInSubFrames: false,
  webSecurity: true,
  allowRunningInsecureContent: false,
  experimentalFeatures: false,
  enableBlinkFeatures: "",   // none
  spellcheck: false,
}
```

`WindowManager.test.ts` asserts these settings at runtime; drift fails CI.

### Content Security Policy (`window-policy.ts::DEFAULT_CSP`)

Strict CSP enforced on every renderer response via the
`onHeadersReceived` hook in `WindowManager.ts`:

```
default-src 'self';
script-src 'self';
style-src 'self' 'unsafe-inline';
img-src 'self' data:;
font-src 'self';
connect-src 'self' http://127.0.0.1:* http://localhost:*
                   ws://127.0.0.1:*  ws://localhost:*
                   wss://127.0.0.1:* wss://localhost:*;
object-src 'none';
base-uri 'self';
form-action 'self';
frame-ancestors 'none';
```

Companion hardening headers also land on every response:
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`.

`'unsafe-inline'` for styles is a temporary concession to Tailwind's
runtime CSS-variable theming; nonce-based hardening is tracked as a §5.4
follow-up. Inline scripts are always forbidden — no `eval`, no
`new Function()`, no `<script>` with body.

### Preload typed API (`apps/desktop/electron/preload.ts`)

The single bridge from renderer → main is `window.hoopoe`. Every method
routes through `ipcRenderer.invoke` to a typed channel name allowlisted
in `apps/desktop/src/shared/ipc-contract.ts`.

```ts
window.hoopoe = {
  daemon:      { request, subscribe },
  settings:    { get, set, watch },
  keybindings: { compile, dispatch },
  approvals:   { listPending, approve, deny, extend },
  files:       { openExternal, revealInFinder, ripgrep },
};
```

Defense-in-depth: even if a renderer plugin somehow constructs a
non-allowlisted method/topic string at runtime, the preload's
`isDaemonRequestMethod` / `isDaemonSubscribeTopic` runtime guards reject
before any IPC fires. The main-side `IpcRegistry` (next section)
re-checks the allowlist as a second wall.

What is **not** exposed:
- direct `fs` / `net` / `child_process` access
- the bearer token, pairing token, WS-token, SSH passphrase
- arbitrary shell execution (Guardrail #2)
- `process`, `require`, or any Node global

### Allowlisted navigation origins (`window-policy.ts::isAllowedNavigationUrl`)

The renderer can only navigate to:
- `http://127.0.0.1` / `http://localhost` (Vite dev + bundled assets)
- `https://127.0.0.1` / `https://localhost`
- `file://` (production bundle)

`will-navigate` and `setWindowOpenHandler` reject anything else.

### Lint rules (`scripts/rendererlint/check-renderer-isolation.ts`)

The renderer-isolation lint runs in `bun run lint` and `bun run test`.
It walks `apps/desktop/src/renderer/**` and refuses:
- imports of `fs`, `net`, `child_process`, `electron`, `electron/renderer`
- `window.require`, `window.process`, `globalThis.require`
- `new Function(...)`, `eval(...)`
- raw `<iframe>` / `<webview>` (allowlisted embeds only — none in v1)

A failing lint blocks merge. The same script is also wrapped by
`scripts/codex-shape-scrub/` to catch §14 "lifted code carries
Codex-shaped assumptions" hazards.

### Security events on rejected privileged ops (`apps/desktop/src/main/IpcRegistry.ts::IpcSecurityEvent`)

When a dispatch reaches the registry with a non-allowlisted, unknown,
or when-clause-blocked command id, the registry emits an
`IpcSecurityEvent` to a configurable sink before rethrowing the
`IpcContractError` / `UnknownIpcCommandError` /
`IpcCommandUnavailableError`:

```ts
interface IpcSecurityEvent {
  kind: "channel-not-allowlisted"
      | "command-not-registered"
      | "command-not-eligible";
  commandId: string;
  missingContextKeys?: readonly string[];
  stage: "register" | "dispatch";
}
```

Higher-level wiring (BackendLifecycle / main bootstrap) attaches the
sink to the structured logger emitting at `warn` level with
`subsystem: "ipc.security"`. The audit log (hp-g73) receives the same
event via the same logger envelope. Renderer attempts to drive
privileged commands therefore appear in:

1. The structured log file under `~/.hoopoe/logs/desktop.main-<date>.log`.
2. The Diagnostics audit-log explorer (hp-1wg8) filterable by
   `subsystem=ipc.security`.
3. The Activity panel banner if the rejection rate breaches a threshold
   (post-MVP rate-monitoring task).

`IpcRegistry.security.test.ts` pins all four rejection paths plus the
sink-throws-don't-block-the-registry-throw safety property.

### Threat model summary

| Threat | Defense |
| --- | --- |
| Compromised renderer reads filesystem | `nodeIntegration: false`, `sandbox: true`, no `fs` import |
| Renderer escalates to arbitrary IPC | `isAllowedRegistryCommandId` allowlist (preload + registry, two walls) |
| Inline script via XSS in design system | `script-src 'self'`; `'unsafe-inline'` not granted to scripts |
| Cookie / token theft via injection | `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `frame-ancestors 'none'` |
| Cross-window navigation hijack | `will-navigate` allowlist + `setWindowOpenHandler` reject |
| Drive-by `<iframe>` injection | `frame-ancestors 'none'` + lint rule + DOM allowlist |
| Codex-shape provider SDK leak | `scripts/providerlint/` + `scripts/codex-shape-scrub/` (CI gates) |
| Provider API key in config | Static lint refuses `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` (Guardrail #11) |

## Daemon Bind Safety

The daemon defaults to `127.0.0.1` and is expected to be reached through the
desktop-managed SSH tunnel. Public or LAN binding is an advanced mode and is not
the v1 happy path.

Any non-loopback, non-tailnet bind must satisfy both checks:

- an explicit config flag or startup flag enables public binding
  (`-allow-public-bind`);
- a runtime confirmation token authorizes that exact bind address.

If either check is missing, the daemon must not listen on the requested public
address. It should fall back to the same port on loopback and emit a structured
warning with `security.public_bind` so Diagnostics can show the red banner.

Tailnet binds are currently recognized as Tailscale addresses in `100.64.0.0/10`
or `fd7a:115c:a1e0::/48`. They do not count as public exposure, but they still
sit outside the first-run SSH-tunnel path and should remain opt-in.

When a public bind is actually authorized, Diagnostics still shows:

> Daemon is bound to `<interface>:<port>`. Public exposure is high-risk. Verify
> mTLS is configured, firewall rules restrict access, and that this is
> intentional.

The dismissal is per bind event. Restarting the daemon creates a new event and
the warning reappears.

Diagnostics reads the current decision from `GET /v1/security/bind-safety`.
That response includes the requested address, effective address, whether the
runtime confirmation succeeded, and the warning payload to display. Startup also
logs the same warning fields through the daemon logger as
`security_public_bind_warning`.

Runtime confirmation tokens are HMAC-signed for the `daemon.public_bind`
operation, scoped to the exact normalized bind address, time-limited, and
consumed once by the daemon process.
