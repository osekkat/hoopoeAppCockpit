# UBS (Ultimate Bug Scanner) integration contract

> First-pass bug scanner for the Debugging / Hardening review rounds (`plan.md` §7.4.2 Round 0 + Round 5; §9.2). Findings flow into the §9.3 finding ledger with `source: ubs` stamped for cross-tool deduping.

## Source of truth

| Field    | Value                                                            |
| -------- | ---------------------------------------------------------------- |
| Tool     | `ubs`                                                            |
| Repo     | TBD (pin on VPS)                                                 |
| Observed | UBS Meta-Runner v5.2.29 (research-spike 2026-05-02 dev box)      |
| Min compatible | 5.0+ (SARIF fusion, `--ci` mode, `--only` lang filter)     |
| Reference | `AGENTS.md` "UBS — Ultimate Bug Scanner" section                |

## Adapter precedence (per `plan.md` §2.3)

1. **`ubs --ci --fail-on-warning <paths>`** — Round 0 batch run (CI mode; deterministic exit code).
2. **`ubs <files>`** — per-file scan (fast).
3. **`ubs --only=<langs> <paths>`** — language-filtered batch.
4. **SARIF output** (when supported) — pulled by the daemon for finding ledger.

## Capability IDs (per `plan.md` §2.8)

| capId                       | Required by                              | Surface                                              | Notes                                              |
| --------------------------- | ---------------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| `ubs.scan`                  | Review round 0 + round 5; specialized audits | `ubs <paths>`                                    | Languages: JS/TS, Python, C/C++, Rust, Go, Java, Ruby, Swift, C#, Elixir |
| `ubs.scan.ci`               | CI-mode pre-PR gate                      | `ubs --ci --fail-on-warning <paths>`                 | Exit non-zero on warning                           |
| `ubs.help`                  | Adapter probe                             | `ubs --help`                                         |                                                     |

## Command surfaces (observed)

| Label              | argv                                          | Exit                  | Notes                                                          |
| ------------------ | --------------------------------------------- | --------------------- | -------------------------------------------------------------- |
| `help`             | `ubs --help`                                  | 0                     | Banner + supported langs; lists `--ci`, `--only`, `--format`.  |
| `scan_files`       | `ubs <file> [<file>...]`                      | 0 if clean / >0 with findings | Output: text or JSON / SARIF (per `--format`).         |
| `scan_ci`          | `ubs --ci --fail-on-warning <paths>`          | 0 / >0                | Used in CI; treats warnings as blocking.                       |
| `scan_lang`        | `ubs --only=ts,go src/`                       | 0 / >0                | Filters by language for speed.                                  |
| `scan_dir`         | `ubs .`                                       | 0 / >0                | Whole-tree scan; slow on monorepos.                             |

## Findings shape (text mode, observed)

```
Warning  Category (N errors)
    file.ts:42:5 - Issue description
    Suggested fix
Exit code: 1
```

### Findings shape (SARIF mode — pin schema on VPS)

UBS supports SARIF output via `--format sarif`. The daemon ingests this for the §9.3 finding ledger. The SARIF top-level fields used:

- `runs[].tool.driver.name` = `ubs` (or sub-scanner)
- `runs[].results[].ruleId` = `<category>`
- `runs[].results[].level` = `note | warning | error`
- `runs[].results[].locations[].physicalLocation.artifactLocation.uri`
- `runs[].results[].message.text`

Hoopoe normalizes SARIF results into the finding ledger's internal shape (`{id, source: 'ubs', category, severity, file, line, col, message, suggestion}`).

## Failure modes & recovery

| Symptom                                              | Root cause                                       | Hoopoe response                                                        |
| ---------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------------- |
| `ubs --ci` exit non-zero                             | Findings present                                 | Aggregate into finding ledger; never fail the swarm — surface for triage. |
| Scan timeout                                         | Very large file or recursive symlink             | Adapter sets per-file timeout; skip+report.                            |
| False positive cluster                               | UBS rule too broad                                | User marks finding as `false_positive` in the ledger; never silenced globally. |
| `ubs` not on PATH                                    | ACFS install incomplete                          | Round 0 reports `ubs.scan` missing; user prompted in Hardening tab.    |

## Authentication / credentials

- None.

## Known gotchas

See [`../research-spike/gotchas.md`](../research-spike/gotchas.md) (`hp-d54`).

- **Always pass changed files** instead of `.` when possible — full-tree scans can take 30+ s on large repos.
- `--only=ts,go` is 3-5× faster than no filter when known.
- UBS uses an internal "shadow workspace" (`/tmp/tmp.XXXXXX/files_scan`) — scanning generated files leaks to the workspace; reserve `~/.cache/ubs/` from Hoopoe cleanup logic.
- Bash files are **not** in UBS's language list (JS/TS, Python, C/C++, Rust, Go, Java, Ruby, Swift, C#, Elixir). Don't expect findings from `*.sh`.

## Test fixtures

| Scenario | Fixture path                                                              | Asserts                                                                 |
| -------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `fresh`  | `packages/fixtures/phase0-2026-05-02/scenarios/fresh/snapshot.json`       | `ubs --ci .` exit 0 (clean fresh project).                              |
| `active` | `packages/fixtures/phase0-.../scenarios/active/snapshot.json`             | 1+ finding from real code; SARIF output captured.                       |
| `failure`| `packages/fixtures/phase0-.../scenarios/failure/snapshot.json`            | Critical finding; finding-ledger entry produced.                        |

## Adapter notes (Hoopoe Go side)

- Lives at `apps/daemon/internal/adapters/ubs/` (Phase 12, bead `hp-rmv`).
- Round runner (`hp-8xm`) invokes UBS as Round 0 and Round 5 (hotspot-targeted); both via `--ci` mode for deterministic exit codes.
- SARIF normalization → finding ledger: every UBS finding gets `source: ubs` so cross-tool deduping (UBS + specialized audits) works.
- Adapter caches per-file results keyed by content hash; same file unchanged → no re-scan.
