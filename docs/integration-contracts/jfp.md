# `jfp` (Jeffrey's Prompts, ACFS-installed) integration contract

> **Free fallback** for skill install / update when [`jsm`](jsm.md) is unavailable or unconfigured. Sufficient for the open-source skills the default tending jobs require. Hoopoe's skill loader tries `jsm` first, then `jfp` (`plan.md` §17, §10.3).

## Source of truth

| Field    | Value                                                            |
| -------- | ---------------------------------------------------------------- |
| Tool     | `jfp`                                                            |
| Source   | Installed by ACFS (`acfs.manifest.yaml`)                         |
| Observed | Not on PATH on dev box (research-spike 2026-05-02 self-test)     |
| Min compatible | TBD                                                       |

## Adapter precedence (per `plan.md` §2.3)

1. **`jsm`** preferred (see [`jsm.md`](jsm.md)).
2. **`jfp install <skill>`** — free fallback.
3. **`jfp list --json`** — enumeration.
4. **`jfp update <skill>`** — controlled upgrade.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `jfp.skill.install`         | First-run wizard (no jsm path)           | `jfp install <skill>`                                | Advisory version-string only; no SHA pin.          |
| `jfp.skill.list`            | Settings → Skills tab                    | `jfp list --json`                                    |                                                     |
| `jfp.skill.update`          | Quarterly upgrade flow                   | `jfp update <skill>`                                 |                                                     |
| `jfp.help`                  | Adapter probe                             | `jfp --help`                                         |                                                     |

## Command surfaces (planned — pin on VPS)

| Label    | argv                                  | Exit | Notes                                                          |
| -------- | ------------------------------------- | ---- | -------------------------------------------------------------- |
| `help`   | `jfp --help`                          | 0    | Adapter probe.                                                 |
| `list`   | `jfp list --json`                     | 0    | Array of installed skills with version strings (no SHAs).      |
| `install`| `jfp install <skill>`                 | 0/1  | Mutating; not invoked by snapshot.sh.                          |

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| Skill not in `jfp` catalog                           | Skill is premium-only via `jsm`                  | Surface to user: "premium subscription required" with link to dashboard. |
| `jfp` not on PATH                                    | ACFS install incomplete                          | Both `jsm` and `jfp` missing → tending jobs cannot boot; block.        |
| `jfp` version drift                                   | `jfp` advisory versioning is loose               | Adapter records the advisory string; cannot verify integrity (see Known gotchas). |

## Authentication / credentials

- None. Free, no subscription.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **Advisory versioning, not SHA-pinned.** A skill version `"vibing-with-ntm-1.4.0"` from `jfp` is not byte-equivalent across installs; subtle skill-content drift is possible. `jsm` is the integrity-guaranteed path.
- The skill *content* between `jsm` and `jfp` is the same for open-source skills — only the integrity guarantee differs.
- Adapter MUST set `sha256: null` in `.hoopoe/skills.lock.json` for `jfp`-sourced skills (see `jsm.md` lock-file shape).

## Test fixtures (placeholder — VPS pin)

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `jfp` present; default tending skills installed via fallback path.      |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | Skills loaded from `jfp` (no jsm subscription); lock file `sha256: null`.|
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Both `jsm` and `jfp` missing; tending jobs blocked.                     |

## Adapter notes (Hoopoe Go side)

- Current checkout: `apps/daemon/internal/skills/` contains the live lock-file,
  digest, and loader substrate for resolving pinned or fallback skills.
- Future Phase 10 target: a CLI adapter package may live at
  `apps/daemon/internal/adapters/skills/jfp/` when bead `hp-4d7` wires direct
  `jfp` command execution.
- Skill loader logic: try `jsm install <skill> --version <sha>`; on 401 / not-installed, try `jfp install <skill>`; on both fail, fail tending boot loudly.
- The lock-file `source: 'jsm' | 'jfp'` field is the audit trail.
