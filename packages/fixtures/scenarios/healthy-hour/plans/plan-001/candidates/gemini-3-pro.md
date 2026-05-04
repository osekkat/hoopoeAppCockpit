# Candidate — Gemini 3 Pro (Deep Think)

## Recommendation: Native macOS app + sidecar daemon

Native Swift + SwiftUI cockpit on macOS, talking to the same Go daemon over a Unix socket
(when local) or SSH tunnel (when remote). Provider integration through CAAM only —
subscription-only matches the Anthropic-Codex-Gemini reality.

## Strengths

- Best macOS look-and-feel (Liquid Glass / system materials are native).
- No Electron memory cost.
- Tighter integration with macOS Keychain, sandbox profiles, and Shortcuts.

## Weaknesses

- Cross-platform port (Linux/Windows) becomes effectively a rewrite.
- SwiftUI's data flow is harder to share with web demos.
- Hiring/contributor pool for SwiftUI engineering is smaller than React.
- Loses the t3code lift entirely — restart from blank.

## Cost: ~$440-$656/month (same as Claude proposal — daemon side identical).

## Quality dimensions

| Dimension       | Score | Notes                                                   |
| --------------- | ----- | ------------------------------------------------------- |
| Vision clarity  | 8/10  | Clear native focus, but narrower target audience.       |
| Risk coverage   | 7/10  | Loses lift; everything hand-built.                      |
| Phase sequence  | 6/10  | Native rewrite blows up Phase 1 timeline.               |
| Test strategy   | 7/10  | XCTest harness viable but loses Vitest reuse.           |
| Cost realism    | 9/10  | Same as Claude.                                         |
