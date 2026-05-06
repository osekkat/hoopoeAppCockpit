# Onboarding

`plan.md §6` defines the first-run and project onboarding flow. This document
holds the living details for `project.json`, readiness checks, checkpoints, and
Mock Flywheel development mode.

## Onboarding Modes

| Mode | Purpose | Default |
| --- | --- | --- |
| Existing VPS | Pair Hoopoe with a VPS the user can already SSH into, install/verify ACFS, then install the daemon. | Yes |
| Mock Flywheel | Fixture-backed local demo/development mode with no real VPS. | Development only |
| Provider provisioning | Create a VPS through a provider plugin, then run the existing-VPS flow. | Post-existing-VPS path |

Existing-VPS stays first. Provider automation must not block manual VPS
onboarding (`plan.md §6.2`, Guardrail 6).

## Guided Tour State

After the first-run wizard reaches the success step, the renderer opens the
guided onboarding tour once unless the user has already skipped or completed
it. The tour state is client UI state, not canonical project state.

| Field | Meaning |
| --- | --- |
| `firstRunCompletedAt` | Timestamp recorded when the wizard success CTA enters the cockpit. |
| `onboardingTourOpen` | Whether the overlay is currently visible. |
| `onboardingTourStepId` | Last viewed tour step; used for resume. |
| `onboardingTourSkippedAt` | Timestamp for an explicit skip. |
| `onboardingTourCompletedAt` | Timestamp for finishing all steps. |
| `onboardingTourLastOpenedAt` | Last manual or automatic launch timestamp. |

Diagnostics exposes a "Start onboarding tour" / "Resume onboarding tour"
button. This is the supported relaunch path for users who skip the tour during
setup but later want the walkthrough.

The step order mirrors the product flow:

1. Top bar status pills
2. Activity drawer
3. Stage rail
4. Planning
5. Beads
6. Swarm
7. Hardening

The tour is informative only. It must never gate readiness, daemon pairing,
project import, or Mock Flywheel access.

## `project.json`

`project.json` lives in the Hoopoe project metadata area for each imported
project. Its schema is owned by `packages/schemas`; this is the operational
field guide.

```json
{
  "schemaVersion": 1,
  "projectId": "hoopoe-app-cockpit",
  "displayName": "Hoopoe App Cockpit",
  "origin": "https://github.com/osekkat/hoopoeAppCockpit.git",
  "vpsRepoPath": "/data/projects/hoopoeAppCockpit",
  "desktopMirrorPath": "~/Library/Application Support/Hoopoe/projects/hoopoe-app-cockpit/repo",
  "planRoot": ".hoopoe/plans",
  "beadsPath": ".beads",
  "defaultBranch": "master",
  "capabilitySnapshotId": "cap-20260504T000000Z",
  "readiness": {
    "phase": "ready",
    "lastCheckedAt": "2026-05-04T00:00:00Z"
  }
}
```

Secret values are not stored here. SSH passphrases, bearer tokens, and provider
credentials live in Keychain/CAAM, referenced by opaque ids only.

## Readiness Checks

| Check | Source | Blocks ready? | Repair surface |
| --- | --- | --- | --- |
| VPS SSH reachable | desktop tunnel manager / daemon bootstrap | Yes | Wizard retry and Diagnostics |
| Daemon installed and healthy | `GET /health`, `GET /v1/version` | Yes | Daemon install/upgrade repair |
| Pairing/bearer valid | auth service | Yes | Re-pair flow |
| ACFS installed | bootstrap parser + `acfs doctor --json` | Yes | Resume bootstrap |
| Tool inventory green/degraded | inventory service + capability registry | Degraded tools may warn | Tool inventory screen |
| CAAM has at least one subscription CLI | CAAM adapter | Warn, do not block | Account setup links |
| `br`, `bv`, NTM, Agent Mail reachable | adapters + capabilities | Blocks relevant stages | Diagnostics repair |
| Project repo imported | Git adapter / `ru` read model | Yes for project stages | Import/reclone |
| Desktop mirror fetched from origin | clone sync | Warn/degraded | Re-fetch mirror |
| Plan root exists | repo filesystem | Blocks Planning lock/convert | Create plan root |
| Beads read model valid | `br --json`, `.beads/issues.jsonl` | Blocks Beads/Swarm | Rebuild bead model |

Readiness is a snapshot. Reconnect, resume, and manual "reload from tools"
actions must re-read canonical surfaces instead of trusting stale cache.

## Bootstrap Checkpoints

The daemon persists checkpoints so a failed or interrupted install resumes
instead of restarting from phase 1:

1. `pre-flight-passed`
2. `packages-installed`
3. `acfs-installed`
4. `acfs-doctor-passed`
5. `daemon-installed`
6. `daemon-paired`

Each checkpoint stores timestamp, actor, command provenance, raw-log path,
structured parser confidence, and repair action ids. Parser confidence loss
falls back to raw-log mode while preserving exit code and resume action.

## Wizard Step Mapping

The first-run wizard wraps the canonical ACFS 13-step path with Hoopoe-specific
checks:

| Step | Hoopoe addition |
| --- | --- |
| Welcome / project choice | Existing VPS first; Mock Flywheel for development. |
| SSH key | Keychain-backed profile reference, no passphrase in cache. |
| VPS connect | Tunnel manager FSM, TOFU known-hosts, health probe. |
| Preflight | Readiness checks and repair actions. |
| ACFS install | Structured stream parser with raw-log fallback. |
| Tool inventory | `br`, `bv`, NTM, Agent Mail, CAAM, caut, DCG, rch, and health adapters. |
| Daemon install | Signed binary verification, systemd install, loopback bind. |
| Pairing | Pairing token -> bearer -> WS-token. |
| Project import | VPS repo path plus desktop origin mirror. |
| Success | Optional tutorial launch; not required for ready state. |

`docs/wizard.md` contains the UI-facing step contract.

## Demo Project Path

The local demo path uses Mock Flywheel fixtures so the user can inspect the
cockpit without a paired VPS. The demo project must preserve the same source of
truth boundaries as a real project:

- fixture snapshots stand in for canonical daemon/tool reads;
- renderer stores are still caches and UI preferences;
- no provider SDKs or direct model APIs are introduced;
- terminal panes remain Diagnostics-only;
- every displayed automation event has replayable fixture evidence.

The demo is acceptable when a new user can open Planning, Beads, Swarm,
Hardening, Activity, and Diagnostics from fixture data and then relaunch the
guided tour from Diagnostics.

## Cross-References

- `plan.md §6` — onboarding strategy and ACFS bootstrap.
- `plan.md §2.5` — connection manager and reconnect ladder.
- `plan.md §6.5` — daemon least-privilege user and setup helper.
- `docs/source-of-truth.md` — persistent data paths.
- `docs/security.md` — secrets, approvals, audit.
- `docs/testing.md` — Phase 3 acceptance and e2e evidence.
