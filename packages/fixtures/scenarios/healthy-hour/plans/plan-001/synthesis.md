# Synthesis — best of all worlds

Adopt Claude's cockpit-first frame as the spine. Reject BYOK (delegate to CAAM, smallest
threat surface). Reject SwiftUI rewrite (vendor t3code Electron scaffolding instead;
re-evaluate after v1 ships). Carry GPT-5 Pro's web-first reflex as a *deferred* future
(post-v1.5 if user demand warrants).

## Locked decisions

1. **Shell:** Electron + TypeScript + React + Vite + Tailwind. Lift `t3code` Electron
   scaffolding (auth, settings, keybindings, build pipeline). Decompose t3code's 2,175-line
   `main.ts` into `BackendLifecycle`, `UpdateMachine`, `IpcRegistry`, `WindowManager`,
   `SettingsBridge`, `AuthBridge` on day one.
2. **Daemon:** Go (chi/echo + modernc SQLite + gorilla/nhooyr WS + creack/pty).
3. **Schemas:** OpenAPI source-of-truth → TS client + Go types. No hand-maintained dupes.
4. **Provider model:** Subscription-only. CAAM is the sole credential pathway. CI fails on
   provider-SDK import.
5. **Phase 0 first.** Real ACFS VPS → JSON snapshots → parser fixtures before any adapter
   ships.

## Rejected alternatives

- **BYOK** — adds credential-storage threat surface and forces parallel SDK integration.
- **Tauri shell** — Rust skill matters less than the t3code lift's velocity.
- **Native SwiftUI** — wrong sequencing; effective Phase-1 rewrite.
- **Browser-first cockpit** — defer to post-v1.5 if user demand warrants.
