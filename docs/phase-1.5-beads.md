# Phase 1.5 Beads

Filed/verified by p4 (FuchsiaPond) from `docs/post-phase1-reality-check.md`
and unresolved HIGH entries in `review-findings.md`.

## Reality-Check Top 5

| Bead | Description | Parent gap |
| --- | --- | --- |
| `hp-vtwm` | Real-VPS Phase 0 acceptance evidence pack: commit fresh/active/failure ACFS snapshots and update contracts for live-shape drift. | Reality-check Top 5 #1, `docs/post-phase1-reality-check.md:190` |
| `hp-2qgx` | Wire the registry-backed CommandPalette into the shell and make Playwright fail if Cmd/Ctrl+K is inert. | Reality-check Top 5 #2, `docs/post-phase1-reality-check.md:191`; review HIGH command-palette finding |
| `hp-411d` | Make root `bun run e2e` real and portable, with deterministic host-dependency handling and structured evidence. | Reality-check Top 5 #3, `docs/post-phase1-reality-check.md:192` |
| `hp-2ae3` | Prove signed/notarized DMG and mock-update acceptance instead of only pipeline scaffolding. | Reality-check Top 5 #4, `docs/post-phase1-reality-check.md:193` |
| `hp-0non` | Make Mock Flywheel drive visible stage UI end-to-end rather than static renderer placeholders. | Reality-check Top 5 #5, `docs/post-phase1-reality-check.md:194`; review HIGH Mock Flywheel/E2E findings |

## Review HIGH Follow-Ups

| Bead | Description | Parent gap |
| --- | --- | --- |
| `hp-6obn` | Wire SettingsBridge security-relevant setting changes into SettingsAuditTrail with tests for audited and non-audited keys. | `review-findings.md`: SettingsBridge audit HIGH |
| `hp-n5za` | Harden preload daemon request/subscribe allowlists and IpcRegistry command/topic typing until generated schemas close the gap. | `review-findings.md`: preload allowlist HIGHs + IPC dispatch HIGH |
| `hp-k9ys` | Strengthen AuthBridgeRedactedError detection for opaque HMAC/base64url bearer and WS tokens. | `review-findings.md`: AuthBridge redaction HIGH |

Notes:
- The five reality-check beads already existed when p4 started this pass; p4 normalized them to P1 and added dependency edges where `br` accepted them.
- p4 created three additional P1 beads for unresolved HIGH review findings that were not fixed inline and not already covered by the reality-check beads.
