# ADR-0002: Signed DMG via website is the v1 distribution channel

- **Status:** Accepted (v1)
- **Date:** 2026-05-04
- **Tracking bead:** hp-kck
- **Related plan sections:** §2.5 (SSH tunnel), §5.3 (desktop shell boundary), §11 (packaging and updates), §13 (MVP scope)
- **Supersedes:** —
- **Superseded by:** —

## Context

Hoopoe v1 is a macOS Electron cockpit that must bootstrap and maintain an SSH
tunnel to the user's VPS. The desktop runs SSH for this one transport purpose,
uses Keychain-backed secret storage, opens localhost daemon HTTP/WS streams, and
updates itself through the release pipeline described in plan.md §11.

The unresolved distribution question was whether v1 should ship through a
signed/notarized DMG outside the Mac App Store, or through the Mac App Store
(MAS). MAS distribution would add sandboxing, entitlement review, update
constraints, and store-review policy risk to the same release where Hoopoe is
trying to prove the SSH tunnel, daemon pairing, local clone mirror, and
subscription-backed planning flows.

The plan already lists Mac App Store distribution in §13 "Can defer", while
§11 specifies a macOS signed and notarized DMG with `electron-updater` against
GitHub Releases for v1.

## Decision

**v1 ships as a signed and notarized DMG distributed from the website and
GitHub Releases. Mac App Store distribution is deferred.**

- Release artifacts are macOS DMG builds for arm64 and x64.
- The updater uses `electron-updater` against GitHub Releases with `latest` and
  `nightly` channels.
- Electron Builder runs with `--publish never`; upload/publish is a separate CI
  step after signing, notarization, and release preflight checks pass.
- MAS submission is not part of v1 acceptance and should not block the release
  pipeline.

## Why not MAS for v1

**SSH bootstrap is the core v1 transport.** Hoopoe's default connection path is
an SSH tunnel from desktop to VPS. MAS sandboxing and entitlement review create
unnecessary uncertainty around subprocess execution, local tunnel listeners,
known-hosts storage, and network behavior. Even if a workable entitlement set is
found later, proving it is not on the critical path for v1.

**The release pipeline already has a clear source of truth.** plan.md §11
defines signed/notarized DMGs, GitHub Releases, channel metadata, and a
mock-update-server path for local testing. Keeping v1 on that path avoids a
second distribution/update channel before the first one is production-proven.

**Existing-VPS-first onboarding should stay boring.** v1's success criterion is
connecting to a real VPS, installing the daemon, pairing, reconnecting, and
driving the Flywheel. Store packaging adds review latency and policy failure
modes that do not improve that first run.

## Implementation contract

The v1 release pipeline owns this decision:

| Surface | v1 behavior |
| --- | --- |
| Desktop package | Signed and notarized DMG, arm64 and x64. |
| Distribution | Website link and GitHub Releases assets. |
| Updates | `electron-updater` generic provider metadata from GitHub Releases. |
| Channels | `latest` and `nightly`, selected through desktop settings. |
| CI | macOS release jobs build/sign/notarize; publish step is separate from build. |
| Local acceptance | Mock update server and mock artifact flows remain supported. |
| MAS | No submission, entitlements, or store-update path for v1. |

## Post-MVP reconsideration

MAS can be reconsidered after v1 if the product needs store distribution enough
to justify a second packaging path. The decision should be revisited only with
answers to these questions:

- Can Hoopoe operate all critical flows under MAS sandbox entitlements without
  weakening the SSH tunnel model?
- Can updater behavior, crash reporting, and diagnostics remain equivalent to
  the GitHub Releases channel?
- Is a MAS-specific build worth maintaining alongside the website DMG?
- Does MAS policy allow the diagnostics, local listener, and SSH bootstrap
  behavior Hoopoe needs?

If the answer is yes, the implementation should be a new v1.x bead set with
explicit acceptance tests. It should not retroactively change v1's release
criteria.

## Consequences

- v1 users install Hoopoe from a website/GitHub Release DMG rather than the Mac
  App Store.
- The team has one update channel to harden before release.
- Enterprise users who only allow MAS-installed software are out of scope for
  v1.
- Release documentation should describe Gatekeeper/notarization behavior, update
  channels, and how to verify the downloaded artifact.
- New contributors should not add MAS entitlements, MAS-specific signing
  profiles, or store submission workflow steps unless a post-MVP bead explicitly
  reopens this decision.

## Validation

This ADR is the acceptance artifact for hp-kck. It maps the bead's definition of
done as follows:

- "ADR-0002 written" — this file.
- "Signed DMG via website for v1; MAS deferred" — Decision and implementation
  contract sections.
- "v1 release pipeline is the implementation" — plan.md §11 remains the release
  source of truth; no new release machinery is introduced here.

## References

- plan.md §2.5 — SSH tunnel as the v1 default transport.
- plan.md §5.3 — desktop runs SSH for the tunnel, not arbitrary project-level
  commands.
- plan.md §11 — packaging and updates.
- plan.md §13 — MVP scope; Mac App Store distribution is in "Can defer".
- docs/development/release-signing.md — current signed DMG and update
  acceptance notes.
