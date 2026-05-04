# Troubleshooting

This runbook favors canonical tool state over Hoopoe cache state. When a
surface looks wrong, reload from the native source first, then inspect Hoopoe's
read model and audit trail.

## First Checks

```bash
git status --short --branch
CI=1 br ready --json
bv --robot-help
rch status
```

Never run bare `bv`; it opens the TUI. Use `bv --robot-*`.

## Common Issues

| Symptom | Likely cause | First action |
| --- | --- | --- |
| `br update` fails with a foreign-key error | Local beads DB write path is inconsistent even if JSONL parses. | Run `br doctor --json`, then `br sync --import-only --json`; avoid `--force` unless a human explicitly asks. |
| Desktop says daemon disconnected after laptop sleep | SSH tunnel died or bearer/WS-token needs refresh. | Reconnect performs tunnel check, bearer check, WS-token refresh, sequence replay, then snapshot. |
| Swarm dashboard misses events | Client sequence cursor fell behind replay retention. | Fetch channel snapshot and inspect `_lag`/`_gap` events. |
| Tool appears installed but feature is disabled | Capability is degraded/missing despite parser success. | Check `/v1/capabilities` and adapter contract test output. |
| Agent holds stale reservation | Agent Mail reservation TTL expired or holder is idle. | Message holder, then use typed force-release action with audit if policy allows. |
| Build/test queue feels stuck | `rch` worker busy or local fallback. | Run `rch status --workers --jobs`; use `rch exec -- <runner>`. |
| Daemon refuses public bind | Loopback policy blocked exposure. | Use SSH tunnel; public bind needs explicit flag and runtime confirmation. |
| Upgrade refused | Checksum, signature, provenance, or SBOM policy failed. | Inspect Diagnostics and `~/.hoopoe/logs`; do not bypass outside dev. |
| Desktop local clone differs from VPS work | Desktop mirror tracks origin, not VPS WIP. | Push VPS commits or view VPS-WIP overlay from daemon RPC. |

## Log Locations

| Surface | Path |
| --- | --- |
| Daemon logs | `~/.hoopoe/logs/` |
| Daemon audit | `~/.hoopoe/audit.jsonl` |
| Bootstrap logs | `~/.hoopoe/logs/bootstrap-<runId>.log` |
| Test evidence | `docs/test-evidence/<phase>/<timestamp>/` |
| Desktop settings | `~/Library/Application Support/Hoopoe/client-settings.json` |
| Desktop mirror | `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/` |

Secrets should not appear in any of these logs. If a token, passphrase, or API
key appears, treat it as a security bug and file a high-priority bead.

## Recovery Rules

- Prefer typed repair actions surfaced by Diagnostics.
- Do not delete project files or clone directories without explicit human
  approval.
- Do not write to the desktop local clone.
- Do not bypass provenance or bind-safety checks in production.
- Do not parse terminal output when a structured API/robot surface exists.

## Cross-References

- `docs/source-of-truth.md` — canonical owners and persistent paths.
- `docs/reconnect-replay.md` — sequence-cursor recovery.
- `docs/security.md` — approval matrix and audit contract.
- `docs/testing.md` — evidence and smoke runners.
