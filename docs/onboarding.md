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
  "vpsRepoPath": "/data/projects/hoopoeAppCockpit/repo",
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

## Cross-References

- `plan.md §6` — onboarding strategy and ACFS bootstrap.
- `plan.md §2.5` — connection manager and reconnect ladder.
- `plan.md §6.5` — daemon least-privilege user and setup helper.
- `docs/source-of-truth.md` — persistent data paths.
- `docs/security.md` — secrets, approvals, audit.
- `docs/testing.md` — Phase 3 acceptance and e2e evidence.
