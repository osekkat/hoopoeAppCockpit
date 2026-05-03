# `phase0-2026-05-02/` — real-VPS fixture corpus

This directory is reserved for Phase 0 acceptance captures from a real
ACFS-installed VPS. The current `.gitkeep` files are placeholders only; the
synthetic Mock Flywheel scenarios under `packages/fixtures/scenarios/` are useful
for development, but they are not Phase 0 acceptance evidence.

The collector for hp-vtwm is:

```bash
scripts/research-spike/collect-evidence-pack.sh \
  --ssh <user@real-acfs-vps> \
  --project-dir /data/projects/<project> \
  --landing-dir packages/fixtures/phase0-2026-05-02/incoming \
  --vps-id research-spike-2026-05-02 \
  --acfs-tag <pinned-acfs-tag> \
  --wizard-command '<13-step wizard transcript command or wrapper>'
```

It produces `/tmp/hoopoe-phase0-evidence-<UTC-timestamp>.tar.zst` containing:

- the hp-6v3 `snapshot.sh` output for `fresh`, `active`, and `failure`;
- the 13-step wizard transcript;
- ACFS version and `acfs doctor --json`;
- versions for `br`, `bv`, `ntm`, Agent Mail, `rch`, `caam`, `dcg`, `caut`,
  `casr`, `srp`, `sbh`, `pt`, `ubs`, `jsm`, and `jfp`;
- per-scenario adapter JSON for the real-VPS evidence pack.

Verify before committing:

```bash
scripts/research-spike/verify-evidence-pack.sh \
  packages/fixtures/phase0-2026-05-02/incoming/<pack>.tar.zst
```

The verifier checks the artifact contract and scans for known secret/token/key
shapes. Only a verifier-passing real-VPS pack should be unpacked and normalized
into `scenarios/fresh/`, `scenarios/active/`, and `scenarios/failure/`.

## Scenarios

| Directory | Source | Status |
| --- | --- | --- |
| `scenarios/fresh/` | `snapshot.sh --scenario fresh` after fresh `acfs onboard` | pending hp-r7i/hp-jvm |
| `scenarios/active/` | `snapshot.sh --scenario active` mid-swarm | pending hp-r7i/hp-jvm |
| `scenarios/failure/` | `snapshot.sh --scenario failure --scenario-notes "<deliberate breakage>"` | pending hp-r7i/hp-jvm |

Do not close hp-r7i, hp-jvm, hp-7cs, or hp-vtwm until a verified real-VPS pack
lands here and the live shapes have been reconciled against the integration
contracts in `docs/integration-contracts/`.
