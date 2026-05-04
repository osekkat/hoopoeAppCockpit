# Unresolved decisions

## Open

1. **Provider plug-in slot for OVH/Contabo/Hetzner** — Phase 13. Existing-VPS first; provider
   automation must not block onboarding (G6).
2. **Local-fallback execution mode** — explicitly out of scope for v1. Re-evaluate if a
   substantial set of users want laptop-only operation.
3. **Multi-VPS / Connection entity migration** — post-MVP per ADR-0001.
4. **mTLS direct-mode transport** — post-v1.x; tailnet-only listener for v1.

## Closed (recently)

- BYOK rejected (synthesis) — CAAM is the sole credential pathway.
- Tauri shell rejected (synthesis) — Electron lift is faster.
- SwiftUI rewrite rejected (synthesis) — wrong sequencing.
- Browser-first cockpit deferred (synthesis) — post-v1.5 if demand warrants.
