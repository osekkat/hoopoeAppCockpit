# `jsm` (Jeffrey's Skills.md CLI) integration contract

> **Preferred** install / update / verify mechanism for skills (`vibing-with-ntm`, `ntm`, `beads-workflow`, etc.). SHA-256 deterministic versioning enables per-project skill-version pinning in `.hoopoe/skills.lock.json` (`plan.md` §10.3, §17). Premium subscription. The free fallback is [`jfp`](jfp.md).

## Source of truth

| Field    | Value                                                            |
| -------- | ---------------------------------------------------------------- |
| Tool     | `jsm`                                                            |
| Source   | <https://jeffreys-skills.md/dashboard> (CLI ships from there)    |
| Observed | `jsm list --json` works on dev box (research-spike 2026-05-02)   |
| Min compatible | TBD (pin on VPS)                                          |

## Adapter precedence (per `plan.md` §2.3)

1. **`jsm install <skill>` / `jsm install <skill> --version <sha>`** — pinned install.
2. **`jsm verify <skill>`** — SHA-256 verification.
3. **`jsm list --json`** — installed-skill enumeration.
4. **`jsm update <skill>`** — controlled upgrade.
5. **Fallback to [`jfp`](jfp.md)** when `jsm` not configured (no premium subscription).

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `jsm.skill.install`         | First-run wizard, project import         | `jsm install <skill> --version <sha>`                | SHA-pinned                                         |
| `jsm.skill.verify`          | Lock-file validation                     | `jsm verify <skill> --json`                          | SHA-256 deterministic                              |
| `jsm.skill.list`            | Settings → Skills tab                    | `jsm list --json`                                    | Returns array of `{name, version, sha256, ...}`    |
| `jsm.skill.update`          | Quarterly upgrade flow                   | `jsm update <skill>`                                 | Optional; user-driven                              |
| `jsm.help`                  | Adapter probe                             | `jsm --help`                                         |                                                     |

## Command surfaces (observed)

| Label             | argv                                       | Exit | Notes                                                          |
| ----------------- | ------------------------------------------ | ---- | -------------------------------------------------------------- |
| `help`            | `jsm --help`                               | 0    | Adapter probe.                                                 |
| `list`            | `jsm list --json`                          | 0    | Array of installed skills with SHAs.                           |
| `verify_help`     | `jsm verify --help`                        | 0    | Captured for adapter contract.                                 |

## `.hoopoe/skills.lock.json` shape (per `plan.md` §10.3)

```json
{
  "schemaVersion": 1,
  "skills": [
    {
      "name": "vibing-with-ntm",
      "source": "jsm",
      "version": "<semver-or-sha>",
      "sha256": "<hex-or-null-when-jfp>",
      "installed_at": "2026-05-03T10:00:00Z"
    },
    {
      "name": "ntm",
      "source": "jfp",
      "version": "<advisory-string>",
      "sha256": null
    }
  ]
}
```

`source` distinguishes which CLI installed the skill so the verify path is right. `sha256` is `null` when only `jfp` is available — adapter must handle both.

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `jsm install` 401 unauthorized                       | No premium subscription                          | Fall back to [`jfp install`](jfp.md); record `source: jfp` in lock.    |
| `jsm verify` SHA mismatch                            | Skill tampered or upstream rev changed           | Block tending job (no skill drift); surface in Diagnostics; user re-installs. |
| `jsm list --json` returns empty                      | Nothing installed yet                            | First-run wizard installs the default-tending set.                     |
| `jsm` not on PATH                                    | Premium not configured / install incomplete      | Fall back to `jfp` automatically; report `jsm.*` capabilities `missing`. |

## Authentication / credentials

- `jsm` uses the user's Jeffrey's Skills.md subscription credentials (not Hoopoe's concern).
- Adapter never reads, copies, or logs those credentials.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **Premium subscription required** — `jsm` fails open to `jfp` for free users. The lock-file records which one was used per skill.
- SHA-256 vs advisory version-string distinction is critical: `jsm` skills have integrity guarantees, `jfp` skills don't. Tending agents loaded from `jfp` are functionally equivalent but cannot be audit-proven against drift.
- `jsm` may sync skills across user devices — Hoopoe should treat the `.hoopoe/skills.lock.json` as the authoritative pin per project; `jsm`'s cross-device sync is orthogonal.

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | Default tending skills installed; `verify` clean.                       |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Mixed sources (jsm + jfp); lock file populated.                          |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | SHA mismatch on one skill; tending blocked until re-install.            |

## Adapter notes (Hoopoe Go side)

- Current checkout: `apps/daemon/internal/skills/` contains the live lock-file,
  digest, and loader substrate for resolving pinned skills at tending-job boot.
- Future Phase 10 target: a CLI adapter package may live at
  `apps/daemon/internal/adapters/skills/jsm/` when bead `hp-4d7` wires direct
  `jsm` command execution.
- Skill loader (`hp-4d7`) prefers `jsm`, falls back to `jfp`.
- `.hoopoe/skills.lock.json` is the source of truth at runtime; the loader
  reconciles installed-set against the lock on every tending-job boot.
- Audit: every install / update / verify / fallback recorded.
