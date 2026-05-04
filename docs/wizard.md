# First-Run Wizard

The wizard is the user's first path to a boring successful run. It wraps the
canonical ACFS bootstrap path with Hoopoe checks, clear repair actions, and
resume checkpoints.

## Principles

- Existing VPS first.
- Provider provisioning is optional and later in the flow.
- Every long-running step has structured progress, raw-log fallback, and a
  resume checkpoint.
- Secrets go to Keychain/CAAM, never to renderer cache or daemon logs.
- The desktop may run SSH for the tunnel only; project-level commands run on
  the VPS through the daemon.

## Step Contract

| Step | User-facing goal | System contract |
| --- | --- | --- |
| Welcome | Choose existing VPS, provider path, or Mock Flywheel. | Existing VPS remains default. |
| Project path | Pick or create the local project record. | No writes to desktop mirror. |
| SSH key | Select key and passphrase reference. | Store secret through Keychain/safeStorage. |
| VPS connect | Host, port, user, TOFU fingerprint. | Tunnel manager opens local loopback tunnel. |
| Preflight | Confirm OS, packages, disk, network. | Structured readiness checks and repair hints. |
| ACFS bootstrap | Install/verify ACFS. | Parser emits phase/checkpoint/fail events; raw-log fallback on drift. |
| Tool inventory | Verify `br`, `bv`, NTM, Agent Mail, CAAM, caut, rch, DCG, UBS, skills. | Capability registry records ok/degraded/missing. |
| Daemon install | Install least-privilege daemon and systemd unit. | Binary verified before install; binds loopback. |
| Pair daemon | Pairing token -> bearer -> WS-token. | Tokens redacted and audited. |
| Project import | Register repo and fetch desktop mirror from origin. | Origin is canonical; VPS WIP comes from daemon overlay. |
| Success | Show ready state and optional tutorial. | Tutorial is optional and not a readiness gate. |

The visual shell lives in `apps/desktop/src/renderer/wizard/`. The substrate
for checkpoints, parser, inventory, and daemon upgrade lives under
`apps/daemon/internal/onboarding/`, `apps/daemon/internal/inventory/`, and
`apps/daemon/internal/upgrade/`.

## Success Handoff

The success CTA records `firstRunCompletedAt` and then routes to the cockpit.
If the onboarding tour has not been skipped or completed, the shell opens the
guided tour at `topbar`. The tour records the last viewed step so closing the
overlay is not destructive; Diagnostics can reopen the same tour later.

The success handoff is intentionally separate from readiness:

- wizard checkpoints prove setup and resume state;
- project readiness comes from daemon/tool/capability checks;
- the guided tour is a UI preference only;
- completing or skipping the tour must not mutate canonical project state.

## Failure Surfaces

| Failure | Surface |
| --- | --- |
| SSH auth failure | Inline error with key/profile repair action. |
| Host fingerprint mismatch | Blocking confirmation; audited if accepted. |
| Bootstrap marker drift | Raw-log mode with parser-confidence warning. |
| Missing subscription CLI | Warning, not blocker. |
| Missing `br`/`bv`/NTM/Agent Mail | Blocks dependent stages until repaired. |
| Daemon provenance failure | Blocking error; no install unless dev override. |
| Mid-install disconnect | Resume from last completed checkpoint. |
| Tour dismissed early | Diagnostics button resumes the last viewed step. |

## Cross-References

- `plan.md §6` — onboarding.
- `plan.md §2.5` — connection manager.
- `docs/onboarding.md` — readiness and project metadata.
- `docs/security.md` — TOFU, secrets, approvals.
- `docs/testing.md` — Phase 3 acceptance tests.
