# Per-tool integration contracts (`hp-78m`)

This directory pins, for every CLI Hoopoe wraps, the **exact command shape, output schema, capability IDs, error modes, and version compatibility** the daemon relies on. Adapter authors write Go from these files alone; the live research-spike VPS is for *grounding* (`hp-r7i` + `hp-jvm`) and *fixture refresh*, not for re-discovery on every adapter.

When the canonical guide (`agent-flywheel.com/complete-guide`) and a referenced skill disagree on a tool's behavior, the skill wins — these contracts capture *Hoopoe's* projection of the tool, not the tool's own documentation. When a contract here and `plan.md` disagree, `plan.md` wins; fix the contract.

## Inventory

| File                              | Tool          | Plan.md ref         | Adapter team owner (Phase) |
| --------------------------------- | ------------- | ------------------- | -------------------------- |
| [`git.md`](git.md)                | `git`         | §2.3, §7.7          | Phase 4                    |
| [`br.md`](br.md)                  | `br`          | §2.3, §7.2          | Phase 6                    |
| [`bv.md`](bv.md)                  | `bv`          | §2.3, §7.2          | Phase 6                    |
| [`agent_mail.md`](agent_mail.md)  | Agent Mail    | §2.3, §7.5, §9      | Phase 9                    |
| [`ntm.md`](ntm.md)                | NTM           | §2.3, §7.3          | Phase 8                    |
| [`ru.md`](ru.md)                  | Repo Updater  | §2.3, §17 (narrow)  | Phase 4                    |
| [`caam.md`](caam.md)              | CAAM          | §7.3, §8.4          | Phase 8                    |
| [`caut.md`](caut.md)              | `caut`        | §7.6, §8.4          | Phase 8                    |
| [`dcg.md`](dcg.md)                | DCG           | §5.3                | Phase 8                    |
| [`casr.md`](casr.md)              | `casr`        | §7.3, §8.4          | Phase 8/Post-MVP           |
| [`pt.md`](pt.md)                  | `pt`          | §8.4                | Phase 10                   |
| [`srp.md`](srp.md)                | `srp`         | §8.4                | Phase 10                   |
| [`sbh.md`](sbh.md)                | `sbh`         | §8.5                | Phase 10                   |
| [`ubs.md`](ubs.md)                | UBS           | §7.4.2, §9.2, §9.5  | Phase 12                   |
| [`jsm.md`](jsm.md)                | `jsm`         | §10.3, §17          | Phase 10                   |
| [`jfp.md`](jfp.md)                | `jfp`         | §10.3, §17          | Phase 10                   |
| [`oracle.md`](oracle.md)          | Oracle        | §7.1                | Phase 5                    |
| [`rch.md`](rch.md)                | `rch`         | §7.3, §8.5          | Phase 8                    |
| [`health/ts.md`](health/ts.md)    | TS/JS health  | §7.4                | Phase 11                   |
| [`health/python.md`](health/python.md) | Python health | §7.4           | Phase 11                   |
| [`health/rust.md`](health/rust.md) | Rust health  | §7.4                | Phase 11                   |
| [`health/go.md`](health/go.md)    | Go health     | §7.4                | Phase 11                   |
| [`health/generic.md`](health/generic.md) | Generic health | §7.4         | Phase 11                   |

## Required shape (every contract file)

1. **Source of truth** — canonical repo, observed version, reference docs.
2. **Adapter precedence** (per `plan.md` §2.3) — ordered list of surfaces, most preferred first.
3. **Capability IDs** declared (per `plan.md` §2.8) — one row per `capId`, with the status semantics and the implementation surface.
4. **Command surfaces** — for every capability, the exact argv / stdin / env / exit-code / stdout / stderr shape. Truncated sample outputs.
5. **Failure modes & recovery** — what the adapter does on each failure class (missing tool, unsupported version, malformed JSON, timeout, high-volume).
6. **Authentication / credentials** — how the adapter inherits credentials (CAAM, env, none).
7. **Known gotchas** — pointer to the entry in [`../research-spike/gotchas.md`](../research-spike/gotchas.md).
8. **Test fixtures** — paths under `packages/fixtures/phase0-*/` that exercise this contract.
9. **Adapter notes** — where the adapter lives (`apps/daemon/internal/adapters/<tool>/`), threading model, idempotency keys, retry policy, redaction expectations.

A contract is **done** when a new adapter author can write the Go from the contract alone, without re-running the research spike.

## Length budget

- Minimum 1 page (no stub contracts).
- Maximum 5 pages (longer = move detail into `docs/research-spike/notes/` or a sub-doc).

## Cross-document conventions

- Tool slugs match the snapshot script: `git`, `br`, `bv`, `ntm`, `agent_mail`, `ru`, `health`, `caut`, `caam`, `dcg`, `casr`, `ubs`, `jsm`, `jfp`, `oracle`, `pt`, `srp`, `sbh`, `rch`.
- Capability IDs follow `<tool>.<surface>.<verb>` — e.g. `bv.robot.plan`, `caam.account.switch`. The capability registry (`plan.md` §2.8) ingests these as keys.
- `blocked-by-policy` is the right status for capabilities that *exist* in the tool but Hoopoe deliberately refuses to call (e.g. bare `bv`, `caam switch-account` outside an arbitration plan, `pt kill` outside `watch-safety-thresholds`).

## Refresh cadence

Update a contract when (any of):

- Snapshot script reports a version bump that changes a capability surface.
- A new fixture under `packages/fixtures/phase0-<date>/` shows new failure modes.
- A capability is added to or removed from `plan.md` §2.8.
- The adapter author hits a real-world divergence and we want the next adapter author to inherit the lesson.
