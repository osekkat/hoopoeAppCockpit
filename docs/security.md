# Security

`plan.md` remains authoritative. This file records implementation guidance that
daemon and desktop code should follow when the plan names a security boundary.

## Daemon Bind Safety

The daemon defaults to `127.0.0.1` and is expected to be reached through the
desktop-managed SSH tunnel. Public or LAN binding is an advanced mode and is not
the v1 happy path.

Any non-loopback, non-tailnet bind must satisfy both checks:

- an explicit config flag or startup flag enables public binding
  (`-allow-public-bind`);
- a runtime confirmation token authorizes that exact bind address.

If either check is missing, the daemon must not listen on the requested public
address. It should fall back to the same port on loopback and emit a structured
warning with `security.public_bind` so Diagnostics can show the red banner.

Tailnet binds are currently recognized as Tailscale addresses in `100.64.0.0/10`
or `fd7a:115c:a1e0::/48`. They do not count as public exposure, but they still
sit outside the first-run SSH-tunnel path and should remain opt-in.

When a public bind is actually authorized, Diagnostics still shows:

> Daemon is bound to `<interface>:<port>`. Public exposure is high-risk. Verify
> mTLS is configured, firewall rules restrict access, and that this is
> intentional.

The dismissal is per bind event. Restarting the daemon creates a new event and
the warning reappears.

Diagnostics reads the current decision from `GET /v1/security/bind-safety`.
That response includes the requested address, effective address, whether the
runtime confirmation succeeded, and the warning payload to display. Startup also
logs the same warning fields through the daemon logger as
`security_public_bind_warning`.
