# Research-spike notes — Phase 0 field notebook

> **Status:** Skeleton. This file is filled in **on the research-spike VPS** during the first end-to-end run of `scripts/research-spike/snapshot.sh` (`hp-6v3`). Until the VPS is provisioned (`hp-r7i`) and the manual 13-step wizard is run (`hp-jvm`), the sections below are placeholders with the right structure.

---

## How to use this file

For every research-spike VPS run, append a new dated subsection to each section below. **Do not overwrite earlier entries** — multiple runs across different VPS hosts and ACFS tags are expected (the fresh / active / failure scenarios in `plan.md` §16 are three runs each).

The structured outputs live in `packages/fixtures/phase0-<date>/`; this file holds the *narrative* — what was tried, what surprised us, what the schema didn't predict.

Cross-references: `plan.md` §16 (Phase 0), §17 (references list), §18.3 (adapter contract tests), `scripts/research-spike/README.md`, `scripts/research-spike/schema/snapshot.schema.json`, `docs/research-spike/gotchas.md` (`hp-d54`).

---

## 0. VPS / ACFS context per run

For each VPS, capture:

| Field                          | Value                                                                |
| ------------------------------ | -------------------------------------------------------------------- |
| Run date (UTC)                 | `YYYY-MM-DD`                                                         |
| VPS provider + plan            | Contabo Cloud VPS 50 / OVH VPS-5 / existing VPS                      |
| `vpsId` (snapshot field)       | `research-spike-<date>` or stable id                                 |
| Region / datacenter            | e.g. EU-Central, US-East                                             |
| OS / kernel                    | Ubuntu 24.x / 25.x; kernel from `uname -r`                            |
| RAM / vCPU / disk              | 64 GB / 16 vCPU / 250 GB NVMe                                        |
| ACFS install command exact     | `curl -fsSL "...install.sh..." \| bash -s -- --yes --mode vibe ...`  |
| ACFS pinned tag                | `--ref <tag>` value (or `main` for ToT spike)                        |
| Install duration (wall)        | minutes                                                               |
| Install resumption count       | how many times the installer was re-run before reporting clean       |
| Tool versions                  | dump from `meta.toolVersions` in the snapshot                        |
| `acfs doctor --json` summary   | green / yellow / red findings                                        |
| Notable manual fixups          | e.g. `cargo install ...`, `bun install ...`                           |

### 2026-MM-DD — placeholder

_(no run yet — pending `hp-r7i`)_

---

## 1. agent-flywheel.com 13-step wizard run-through (`hp-jvm`)

For each step, record:

- Step name + URL fragment.
- Decision taken (which option was selected).
- Time spent.
- Anything ambiguous, missing, or that contradicted assumptions in `plan.md` §6.
- Screenshots / pasted CLI output (committed to `packages/fixtures/phase0-<date>/wizard/<step-N>/`).

Steps to cover:

1. OS selection.
2. SSH key generation.
3. SSH config and first connection.
4. Hostname / locale / timezone.
5. Package upgrades.
6. Firewall posture.
7. User account.
8. Language runtimes (Bun, Rust, Go, Node).
9. Provider account configuration (CAAM).
10. ACFS installer.
11. NTM bootstrap.
12. First project import.
13. `acfs onboard` interactive tutorial.

### 2026-MM-DD — placeholder

_(no run yet — pending `hp-jvm`)_

---

## 2. snapshot.sh run log

Per run, capture:

- Exact CLI flags used.
- Wall-clock duration (also reported in `meta.captureDurationMs`).
- Output file size.
- Stderr summary (any "tool capture failed" lines).
- Diff vs prior run (after normalizing timestamps + durations + host fields):

  ```bash
  jq 'del(..|.durationMs?, ..|.capturedAt?, ..|.host?, ..|.captureDurationMs?, .meta.toolVersions)' \
     scenarios/fresh/snapshot.json | sha256sum
  ```

- Tools that were not on PATH (`present: false`).
- Tools that were on PATH but errored (non-zero exit on every invocation).

### 2026-05-02 — local self-test (this development workstation, not a VPS)

Captured against `/home/ubuntu/Projects/hoopoeAppCockpit` for envelope-shape verification.

- Flags: `--self-test --scenario ad_hoc --vps-id local-self-test`
- Wall-clock: ~5s
- Output: `/tmp/hoopoe-snapshot-full.json`, ~2.6 MB
- `present: true` for: `git`, `br`, `bv`, `ntm`, `agent_mail`, `ru`, `caam`, `dcg`, `ubs`, `jsm`, `health` (no language adapters detected).
- `present: false` for: `caut`, `casr`, `jfp`, `oracle`, `pt`, `srp`, `sbh` (none installed on this dev box).
- Notable: `agent_mail --version` returns the help banner border instead of a version string. → flag for `hp-d54`.
- Notable: `ru --schema` and `ru robot-docs` produced large but well-formed JSON; envelope handled the size without ARG_MAX issues after switching to `--slurpfile`/`--rawfile`.
- Notable: `ubs --ci .` scanned the whole repo (mostly markdown right now); duration acceptable.

This run is **not** committed as a fixture (it is `--self-test`, not a real VPS scenario).

### 2026-MM-DD — fresh scenario

_(no run yet)_

### 2026-MM-DD — active scenario

_(no run yet)_

### 2026-MM-DD — failure scenario

_(no run yet)_

---

## 3. Per-tool field notes

For each tool, capture:

- Exact `--version` string observed.
- Help-text quirks (banners, ANSI, paging).
- Any mismatch between documented flags and actual flags.
- Output destinations that surprised us (JSON to stderr, error text to stdout, exit code 0 on logical failure, etc.).
- Capability IDs that *should* exist but weren't returnable on this VPS (record as `degraded` or `missing` in the snapshot).

### `git`

_(no notes yet — fill in on first VPS run; placeholder so the section is discoverable)_

### `br`

- 2026-05-02 local: `br schema` emits a large JSON document (the JSON Schema used to validate `.beads/issues.jsonl` — *not* the issue data itself). Adapter consumers should use `br list --json` for issue payloads, not `br schema`.

### `bv`

- 2026-05-02 local: `--robot-help` succeeded, lists the documented `--robot-*` surface. Bare `bv` was **never** invoked (Guardrail 1).

### `ntm`

_(no notes yet)_

### `agent_mail`

- 2026-05-02 local: no `--version` flag; `--help` works. `probe_version` fallback caught the help-banner box-drawing border as the "version" — recorded for `hp-d54`.

### `ru`

- 2026-05-02 local: `--schema` is huge (multi-hundred-KB JSON). Consumer code should stream-parse if memory matters.

### `health` (lizard / scc / tokei)

- 2026-05-02 local: `lizard` not on PATH. `scc` / `tokei` checks succeed when present. The capture function is intentionally best-effort — health adapters are language-specific (`hp-9uh`).

### `caut` / `caam` / `dcg` / `casr` / `pt` / `srp` / `sbh`

_(per-tool notes will accrue per real-VPS run; baseline help-shape captured by snapshot.sh on 2026-05-02 local for the ones present)_

### `jsm` / `jfp`

- 2026-05-02 local: `jsm list --json` works; `jfp` not installed locally. Per `plan.md` §10.3, the lock-file shape needs both — see `hp-78m` jsm.md / jfp.md.

### `oracle`

_(only on macOS user host; not captured on Linux research VPS)_

### `ubs`

- 2026-05-02 local: `ubs --ci .` worked end-to-end on the (mostly markdown) Hoopoe checkout.

---

## 4. Open questions / spec gaps

Use this section to log questions that arise during a run that the plan doesn't yet answer. Each question gets a one-line summary + a link to the bead/PR/discussion that resolved it (or "open").

- **2026-05-02:** `--schema-validate` flag depends on `ajv-cli`. `npm i -g ajv-cli` is fine on the dev box; do we want to bundle a Go-based validator into the daemon for CI use? (Open — likely answered when `packages/schemas/` lands per `hp-r3i`.)

---

## 5. Decisions made on the VPS that should land in the plan

If a research-spike run forces a plan edit (a tool surface differs materially from §17, a capability is genuinely missing, a guardrail interpretation needs to be sharpened), record it here AND open a `plan.md` PR. **Do not edit `plan.md` from this notebook.**

_(none yet)_
