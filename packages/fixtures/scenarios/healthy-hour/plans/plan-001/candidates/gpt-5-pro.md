# Candidate — GPT-5 Pro (via Codex CLI)

## Recommendation: Web-first cockpit with desktop wrapper

Build the cockpit as a web app first (React + tRPC) and wrap it in Tauri for desktop. The VPS
daemon exposes an authenticated WebSocket; the same UI runs in a browser tab against a remote
or in a Tauri shell against a local tunnel. BYOK supported for power users who already have
provider API keys.

## Strengths

- Lower total surface — one UI codebase serves web + desktop.
- BYOK welcomes existing API users without forcing them onto subscriptions.
- Tauri's Rust shell is lighter than Electron.

## Weaknesses

- macOS Keychain integration via Tauri is rougher than Electron `safeStorage`.
- BYOK adds a credential-storage threat surface (Hoopoe would own provider keys instead of
  delegating to CAAM).
- Splitting between browser + Tauri increases QA burden.

## Cost: ~$340-$556/month (VPS + Claude Max + optional ChatGPT Pro; BYOK reduces some seats).

## Quality dimensions

| Dimension       | Score | Notes                                                       |
| --------------- | ----- | ----------------------------------------------------------- |
| Vision clarity  | 7/10  | Web-first + desktop wrapper has more moving parts.         |
| Risk coverage   | 6/10  | BYOK threat model under-specified.                         |
| Phase sequence  | 7/10  | Phasing fine, but Tauri integration is a Phase-1 unknown.  |
| Test strategy   | 8/10  | Same Mock Flywheel pattern works.                          |
| Cost realism    | 8/10  | Slightly cheaper, but BYOK savings are user-dependent.     |
