# Comparative matrix — Hoopoe v1 candidates

| Dimension           | Claude (Opus 4.7)            | GPT-5 Pro (Codex)            | Gemini 3 Pro (Deep Think)    |
| ------------------- | ---------------------------- | ---------------------------- | ---------------------------- |
| Shell               | Electron + t3code lift       | Tauri + web fallback         | Native SwiftUI                |
| Daemon              | Go (chi/echo, modernc SQLite)| Go (same)                    | Go (same)                    |
| Provider model      | Subscription-only            | BYOK + subscription          | Subscription-only            |
| Platform v1         | macOS                        | macOS + browser              | macOS                        |
| Cross-platform path | Strip Linux/Windows now      | Already cross via web        | Effective rewrite            |
| Threat surface      | Smallest (CAAM only)         | Larger (BYOK)                | Smallest (CAAM only)         |
| Phase 0 fit         | Strong — fixtures-first      | Strong — same fixtures       | Weaker — must rewrite parsers|
| Cost                | $440-$656/mo                 | $340-$556/mo                 | $440-$656/mo                 |
| Hiring pool         | Large (TS/React)             | Large (TS/React)             | Smaller (Swift)              |

## Verdict (synthesis input)

**Claude** is the strongest baseline: cockpit-first framing, t3code lift, subscription-only
model access, smallest threat surface.

**GPT-5 Pro** contributes the BYOK question (rejected for v1 — delegates to CAAM) and the
web-first reflex (rejected — Electron lift is faster than Tauri).

**Gemini 3 Pro** contributes the native-feel argument (rejected for v1 — Liquid Glass via
Tailwind tokens is enough; SwiftUI rewrite is wrong sequencing).

Synthesis carries forward Claude's structure with explicit rejection notes for the BYOK and
SwiftUI alternatives.
