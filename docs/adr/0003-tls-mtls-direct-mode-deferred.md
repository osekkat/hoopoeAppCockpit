# ADR-0003: TLS / mTLS direct mode and Tailnet mode are post-MVP

- **Status:** Accepted (v1)
- **Date:** 2026-05-04
- **Tracking bead:** hp-7r4
- **Related plan sections:** §2.4 (default network posture), §2.5 (transport ladder), §5.4 (TOFU fingerprint rules), §13 (post-MVP defer)
- **Supersedes:** —
- **Superseded by:** —

## Context

The plan's §2.5 transport ladder ranks four daemon transport modes:

1. **SSH tunnel** (v1 default) — desktop opens a local-port → daemon-127.0.0.1
   tunnel, speaks HTTPS/WS to localhost. Source-of-truth boundary stays clean,
   SSH keys do auth, every NAT/firewall the user already navigated to ssh in
   is supported.
2. **Localhost binding via systemd socket activation** (already in v1).
3. **Tailnet listener** (post-MVP) — daemon listener bound to a Tailscale
   interface; manage `tsnet` sidecar OR connect to the user's already-installed
   Tailscale daemon.
4. **mTLS direct mode** (post-MVP, advanced/team) — daemon exposes HTTPS on a
   public or private interface, client certs pinned at provisioning, bearer
   token still required on top of mTLS, loud diagnostics if exposed publicly.

Bind safety is already enforced in v1 (§2.4): the daemon refuses non-loopback
binds unless launched with `-allow-public-bind` *and* a runtime confirmation
token; the desktop surfaces a `security.public_bind` Diagnostics warning
whenever such a bind is active. mTLS and tailnet modes layer onto that
posture rather than replace it.

## Decision

**v1 ships SSH-tunnel-only.** Tailnet and mTLS direct modes are deferred to
v1.x.

- v1 transport: `transportsecurity.ModeTunnel` is the only mode the daemon
  publicly advertises.
- The capability registry, TOFU policy, public-bind safety, and Diagnostics
  warning surface are scaffolded for direct/tailnet modes today; flipping a
  v1.x release to enable them is a configuration change plus the cert /
  tsnet wiring listed below, not a major rewrite.
- The desktop's wizard does not surface direct or tailnet options for v1.
  Telemetry, audit, and approvals already treat them as recognized but
  blocked-by-policy modes so any accidental enable is loudly visible.

## Why post-MVP

**Operational complexity.** mTLS adds cert distribution, scheduled rotation,
revocation, fingerprint TOFU rules that differ from SSH-bootstrap-derived
TOFU, and a public-binding alarm path that must page the operator if a
private-network assumption is violated. Tailnet is simpler but still adds
either a `tsnet` library dependency or a dependency on a separately-managed
Tailscale daemon, and a network-topology question (which tailnet, which
node, lockdown via tags) that shifts the failure mode from "tunnel down"
to "wrong tailnet identity."

**v1 use case alignment.** v1 is for solo builders and small teams whose
laptops can already SSH into their VPS. The SSH tunnel is the same channel
those users already trust their bootstrap and `git push` over; introducing
a second authenticated transport before the first is shaken out is a
distraction.

**§13 defer rationale.** §13 'Can defer' lists mTLS public mode and tailnet
listener explicitly. The product principle in §1.6 ("make the first
successful run boring") wants existing-VPS-first onboarding to land cleanly
before transport variance enters the matrix.

## Integration shape (frozen for v1.x enable)

The pieces already in v1:

- `apps/daemon/internal/transport/security/tofu.go` — `TransportMode`
  constants (`ssh_tunnel`, `direct`, `tailnet`), TOFU pin verification with
  authenticated-SSH-bootstrap evidence requirement, fingerprint storage.
- `apps/daemon/internal/security/bind.go` — `PublicBindConfirmer` HMAC token,
  `security.public_bind` Diagnostics warning code, `ErrPublicBindNotConfirmed`
  refusal path.
- `apps/daemon/cmd/hoopoed -allow-public-bind` flag with mandatory
  `-public-bind-confirmation-token` runtime token.
- §10.2 Diagnostics red-warning surface that lights up whenever a public
  listener is active.

What v1.x enable adds:

| Surface                                          | v1.x work                                                                                       |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| Cert provisioning                                | Issue, distribute, and pin client certs during onboarding. Tie to bootstrap-token consumption.  |
| Cert rotation                                    | Daemon-side rotation schedule + refresh RPC. Desktop reconnect re-pins on rotation.             |
| TOFU fingerprint policy in direct/tailnet        | Already encoded — TOFU only when fingerprint arrived over an authenticated SSH bootstrap.       |
| Tailnet sidecar                                  | Either `tsnet` Go library link or detection of a running `tailscaled` and bind to its iface.    |
| Bearer-on-top-of-mTLS                            | Existing bearer flow stays — mTLS authenticates the channel, bearer authorizes the principal.   |
| Diagnostics direct/tailnet badge                 | Mirror the public-bind warning with mode-specific copy and a "switch back to SSH tunnel" CTA.   |
| Capability registry                              | Add `transport.direct.enable` / `transport.tailnet.enable` capabilities, gated `blocked-by-policy` until the operator opts in. |
| Wizard surfaces                                  | Step 4 of §6.1 gains a "Choose transport mode" option; non-default modes show the consequences. |
| Audit log                                        | Mode-change events recorded with operator identity + new fingerprint pin.                       |

## Consequences

- v1 cannot serve users on networks where SSH outbound is blocked but HTTPS
  is allowed (rare; mostly enterprise-restricted laptops). Those users wait
  for v1.x.
- The codebase carries a small amount of unused-but-tested transport
  scaffolding (`ModeDirect`, `ModeTailnet` paths in `transportsecurity`).
  This is intentional — the cost of carrying a stub today is far smaller
  than the cost of inventing the integration shape under deadline pressure
  later.
- Documentation, capability registry, and Diagnostics already reference all
  three modes. New contributors should NOT remove `direct`/`tailnet` enum
  values "because they are unused" — the deferral is deliberate.
- v1.x release notes will reference this ADR as the integration contract;
  the v1.x bead set will pick up the enable-time work above.

## Validation

This ADR is acceptance evidence for hp-7r4. The bead's three definition-of-
done items map as follows:

- "documents the integration shape" — this ADR.
- "code paths scaffolded so enabling is a flip rather than a major rewrite"
  — `apps/daemon/internal/transport/security/` (TOFU + mode constants),
  `apps/daemon/internal/security/bind.go` (public-bind safety),
  `apps/daemon/cmd/hoopoed` (`-allow-public-bind` flag with confirmation
  token). Tests live alongside each surface.
- "Diagnostics red-warning surface (already in §10.2)" — `security.public_bind`
  warning code, surfaced today on any non-loopback bind. v1.x extends the
  same surface with mode-specific copy.

## References

- plan.md §2.4 — default network posture.
- plan.md §2.5 — full transport ladder + FSM.
- plan.md §5.4 — TOFU fingerprint rules.
- plan.md §13 — post-MVP defer list.
- ADR-0001 — single-VPS-per-install (related: VPS reach-paths).
- `apps/daemon/internal/transport/security/tofu.go` — TOFU policy (live).
- `apps/daemon/internal/security/bind.go` — public-bind safety (live).
