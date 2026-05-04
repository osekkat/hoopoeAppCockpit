# Daemon Upgrade And Rollback

Daemon upgrades are user-approved, provenance-verified, audited operations.
They are implemented by the daemon because the daemon owns its binary, config,
database backup, systemd lifecycle, and compatibility checks.

## Upgrade Sequence

1. Desktop reads `/v1/version` and the release manifest.
2. Compatibility check verifies the current desktop can speak to the target
   daemon API.
3. User approves the upgrade. Write actions enter read-only upgrade mode.
4. Daemon downloads the candidate binary into `~/.hoopoe/binaries/staging/`.
5. Verifier checks checksum, Ed25519 signature, provenance attestation, and
   SBOM policy.
6. Daemon backs up config and `~/.hoopoe/daemon.db`.
7. `systemctl stop hoopoe` or equivalent setup-helper action stops the service.
8. Binary is installed atomically.
9. Service restarts and emits `sd_notify` readiness where available.
10. Daemon verifies `/v1/version`, migration state, capabilities, and minimum
    desktop compatibility.
11. Read-only upgrade mode exits and the audit entry records success.

If any step after backup fails, rollback restores the previous binary and DB
backup, restarts the old daemon, and records the failure plus restored version.

## Verification Requirements

| Check | Refusal behavior |
| --- | --- |
| SHA-256 checksum mismatch | Refuse install. |
| Signature missing or not from pinned key | Refuse install. |
| Provenance source commit/builder mismatch | Refuse install. |
| Provenance SLSA level below policy | Refuse install. |
| Malformed or missing SBOM | Refuse install. |
| High/critical SBOM finding | Require explicit acknowledgement and audit. |
| Config/DB backup fails | Abort before install. |
| Post-install `/v1/version` fails | Roll back. |
| Migration fails | Roll back binary and DB backup. |

An insecure development override is allowed only for local development. It must
include actor, reason, timestamp, failed checks, and a persistent Diagnostics
warning. Production use with the override is unsupported.

## Audit Fields

```json
{
  "type": "daemon.upgrade",
  "actor": "user",
  "fromVersion": "0.0.5",
  "toVersion": "0.0.6",
  "releaseManifest": "sha256:...",
  "verification": {
    "checksum": "pass",
    "signature": "pass",
    "provenance": "pass",
    "sbom": "pass"
  },
  "backupPath": "~/.hoopoe/backups/daemon-...",
  "outcome": "succeeded"
}
```

Failure entries include the failing step, error class, rollback attempt result,
and final daemon health status.

## Cross-References

- `plan.md §11` — daemon distribution and tool version pinning.
- `plan.md §10.3` — schema migrations and retention.
- `docs/security.md` — release verification and insecure override posture.
- `apps/daemon/internal/upgrade/` — service implementation and rollback tests.
- `apps/daemon/internal/release/` — manifest/provenance/SBOM verifier.
