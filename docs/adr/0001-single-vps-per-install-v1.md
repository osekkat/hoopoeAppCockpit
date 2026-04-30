# Single VPS per Hoopoe install in v1; multi-VPS deferred

In v1 a Hoopoe install pairs with exactly one VPS, and that VPS holds N projects. We rejected building a multi-VPS connection switcher into v1 because it would add a Connection dimension to every screen, settings file, and API path before we have evidence the simpler model is insufficient. When multi-VPS becomes a real need, we will introduce a `Connection` entity that owns VPS state and key projects to it; until then the implicit single Connection has no UI and no schema presence.

## Consequences

- §4.1 in `plan.md` is split into two independent state machines: a VPS lifecycle (`unconfigured → ssh_verified → daemon_running → tools_installed → ready`) and a per-project lifecycle (`imported → planning → ... → completed`). VPS state is a precondition, not a state in the same machine.
- The desktop builds one project switcher and no VPS switcher.
- API paths stay `/v1/projects/{projectId}/...` with no connection segment; adding multi-VPS later means adding a `Connection` resource and either a new path prefix or a header, both of which are additive.
- Schema for `Project` does not carry a `connectionId` field in v1. Adding one later is a migration we accept.
