# Anti-patterns to refuse

> Engine-first slice for **hp-iswv** (Cross-cutting: Appendix B anti-pattern compliance audit).
> This document is the rationale-and-remediation companion to `plan.md` Appendix B
> "Anti-patterns to refuse" (lines 2331-2338). It exists so future contributors
> can see *why* each anti-pattern is forbidden — not just that the CI gate
> rejects it — and so the gate's failure messages can point at a specific section
> here for the remedy.
>
> The CI job `antipattern-compliance` (hp-iswv DOD) asserts each item below;
> a failed grep on any of them is a failed PR. Per `plan.md` Appendix C, a
> failed grep on a guardrail is a failed PR — these anti-patterns are the
> structural complement to those guardrails.

---

## #1 — `PubSub.unbounded` everywhere → bound all channels

**What it looks like.** T3 Code uses `PubSub.unbounded(...)` (and equivalent unbuffered
`make(chan T)` declarations in fan-out paths) so producers never block. A slow consumer
or wedged subscriber accumulates messages without backpressure, and the daemon's RSS
grows until OOM-kill.

**Why Hoopoe refuses it.** §10.1 ("Backpressure rules") and §14 risk #14 require that
*every* event-fan-out channel inside `apps/daemon/internal/` has an explicit bound and
an explicit slow-consumer policy (drop-oldest, drop-newest, lag-event, or close). The
daemon multiplexes WS subscribers, audit fan-out, and tending-job event streams in a
single process; a single wedged renderer cannot be allowed to take the daemon down.

**How CI detects it.**

```bash
rg -n 'PubSub\.unbounded' apps/daemon/internal/        # must be empty
rg -n 'make\(chan [^,)]+\)\s*$' apps/daemon/internal/  # unbuffered chans require a comment
```

The detection rule for `make(chan T)` in fan-out paths: any unbuffered channel in a
file that publishes to >1 subscriber must carry an inline `// rationale: …` comment
explaining why it cannot be bounded. The audit treats a missing rationale as a
regression.

**Implementation owner.** hp-q3p (Bounded-channel + slow-consumer load tests).

**Remedy when triggered.** Replace the unbounded channel with a buffered channel of
documented capacity, and add a slow-consumer policy via the EventHub `LagEvent`/
`GapEvent` mechanism (`apps/daemon/internal/api/events.go`). Add a load test that
asserts daemon RSS stays bounded under a wedged subscriber.

---

## #2 — Terminal history as one big string blob → chunk it

**What it looks like.** T3 Code's `terminal.open` IPC returns the entire scrollback
as one string. For a long-running pane this can be many MB; for a Hoopoe swarm with
20 panes, it is unbounded.

**Why Hoopoe refuses it.** Guardrail 12 already forbids surfacing raw terminal panes in
the default Swarm UI, but Diagnostics' "Show raw pane" toggle is allowed and audited
(§7.3). Even there, the renderer must never receive a multi-megabyte string in one
WS message — it blows the heartbeat budget, stalls the renderer, and (per §10.1)
risks tripping backpressure on the daemon side. The `/v1/jobs/{id}/log?offset=` API
exists specifically so logs are paged.

**How CI detects it.**

```bash
ast-grep run -l Go -p 'PaneAttachResponse{$$$}' apps/daemon/internal/
# Inspect each construction; PaneAttachResponse.Scrollback must carry an
# explicit bound (default 256 KiB ring; see plan.md §2.7) plus an offset
# cursor for paged fetches via /v1/jobs/{id}/log?offset=.
```

Test asserts a PTY-attach payload is `< 512 KiB` even when the underlying pane has
hours of scrollback; older content is fetched via the chunked-log API only when the
user scrolls past the live tail.

**Implementation owner.** hp-9vg (chunked log API) + hp-yf9 (PTY plumbing).

**Remedy when triggered.** Move the full-string return to an offset-based endpoint;
have `terminal.open` return only the bounded ring + the latest offset. The renderer
fetches older content via `GET /v1/jobs/{id}/log?offset=…&limit=…`.

---

## #3 — Silent client-side message caps → virtualize or surface "showing latest N"

**What it looks like.** T3 Code defines `MAX_THREAD_MESSAGES = 2_000` and silently
slices the array in the renderer. The user has no signal that messages were elided;
older context simply disappears.

**Why Hoopoe refuses it.** §1.4 ("every automation must be inspectable") and §7.5
(Activity panel) require that any list surface either (a) virtualizes the full set
(via TanStack Virtual or equivalent) or (b) shows an explicit "showing latest N of M"
indicator with a "load older" affordance. Silent truncation is a §1.4 violation: the
user cannot see that the list is incomplete, cannot see how much was elided, and
cannot reach the elided rows.

**How CI detects it.**

```bash
rg -n 'MAX_(THREAD|LIST|MESSAGE)' apps/desktop/src/renderer/   # justified hits only
ast-grep run -l TypeScript -p '$X.slice(-$N)' apps/desktop/src/renderer/
# Each hit must be either inside a virtualizer or accompanied by a
# "showing latest N of M" UI affordance — verified by snapshot test.
```

Surfaces required by this rule: Activity timeline, Agent Mail timeline, audit log
viewer, finding ledger, beads list, agents list, swarm session list.

**Implementation owner.** none specifically — it's a cross-cutting renderer
discipline; UI beads (hp-v6n / hp-tg6 / hp-k4j / hp-gmm / etc.) each must satisfy it.

**Remedy when triggered.** Replace `array.slice(-N)` with a virtualized list
component, OR keep the slice but add a banner showing `"latest 2,000 of 12,453 —
[load older]"` that fetches the next page from the daemon.

---

## #4 — 2,175-line `main.ts` monolith → decompose on day one

**What it looks like.** T3 Code's `apps/desktop/src/main.ts` is 2,175 lines of
mixed responsibilities: backend lifecycle, electron-updater wiring, IPC handler
registration, window management, settings IO, auth handshake, and a dozen helpers.
It works, but a 2k-line file has no organic boundaries that future maintainers can
draw between concerns; every change touches a global blob.

**Why Hoopoe refuses it.** Inheriting that monolith would re-import every problem
the t3code project is currently working through (testability, hot-reload, code
ownership). Hoopoe's `apps/desktop/src/main.ts` is decomposed on day one into
six Hoopoe-owned integration seams under `apps/desktop/src/main/` —
`BackendLifecycle.ts`, `UpdateMachine.ts`, `IpcRegistry.ts`, `WindowManager.ts`,
`SettingsBridge.ts`, `AuthBridge.ts` — each importing from `vendored/t3code/`
helpers but containing only the orchestration code Hoopoe needs.

**How CI detects it.**

```bash
wc -l apps/desktop/src/main.ts apps/desktop/src/main/*.ts
# main.ts itself: < 200 lines (orchestration only).
# Each main/*.ts: < 800 lines soft, < 1200 lines hard CI fail.
```

Test asserts that all six modules exist as separate files and that none has
acquired more than the soft-cap line count without a tracking bead.

**Implementation owner.** hp-zir (the original decompose pass; landed pre-Phase-1).

**Remedy when triggered.** Split the offending module along the natural seam —
follow the same six-module decomposition pattern. If the seam is non-obvious,
file a follow-up bead before letting the file grow past the soft cap.

---

## #5 — No port-conflict resolution → `findOpenPort(preferred)` in `BackendLifecycle.ts`

**What it looks like.** T3 Code hardcodes a port for the local daemon and crashes
loudly on EADDRINUSE. For a single-instance product that's tolerable; for Hoopoe
it's not — a developer running two cockpit instances pointed at different VPSes
would conflict, as would Hoopoe vs. an existing local-development service.

**Why Hoopoe refuses it.** §1.5 ("build for restartability") requires that the
desktop survives transient environmental failures including port collisions.
`BackendLifecycle` MUST call `findOpenPort(preferred=8765)` and pass the actual
chosen port to the daemon spawn via `HOOPOE_PORT=<port>`. The daemon must emit
`listening on :<port>` on stdout; `BackendReadinessDetector` parses that line
rather than assuming the preferred port.

**How CI detects it.**

```bash
ast-grep run -l TypeScript -p 'spawn($$$, { env: { HOOPOE_PORT: $$$ }})' \
  apps/desktop/src/main/BackendLifecycle.ts
# Argument to HOOPOE_PORT must be a findOpenPort() call, not a literal.
```

E2E test: launch two desktop instances on the same Mac; assert both reach Healthy
Backend with different ports.

**Implementation owner.** hp-zir (BackendLifecycle decomposition).

**Remedy when triggered.** Replace the literal port with `await findOpenPort({
preferred: 8765 })`; thread the chosen value into `HOOPOE_PORT`, the daemon
`SettingsBridge` cache, and the WS connection URL. Re-run the two-instance E2E.

---

## #6 — Implicit string-switch dispatch → real command registry

**What it looks like.** T3 Code dispatches commands via
`switch (commandName) { case 'X': … }` blocks scattered across the main process.
Adding a command means editing the switch; renaming a command means a hunt across
files; testing dispatch in isolation is impossible because the switch is buried
inside event handlers.

**Why Hoopoe refuses it.** §1.4 (inspectability) and §7 (the `commandPalette` /
`CommandKMenu` UI) require that every command is a typed object registered with
the central `commandRegistry`. The registry exposes `invoke(id, payload)`, audits
every invocation, and renders the command palette by enumerating registered IDs.
A string-switch dispatch makes the audit + the palette + the keybinding rebinding
all undecidable.

**How CI detects it.**

```bash
ast-grep run -l TypeScript -p 'switch ($CMD) { $$$ }' \
  apps/desktop/src/main/ apps/desktop/src/renderer/
# Each hit must be inspected; switches over command-name strings are forbidden.
# Allowed: switches over typed enums (e.g., schema-derived status enums).
```

**Implementation owner.** hp-rth (keybindings + command registry).

**Remedy when triggered.** Replace the `switch` with a `commandRegistry.invoke(id,
payload)` call; register each branch as a separate command at module-load time
(typically in the module that owns the command's behavior, not at the dispatch
site).

---

## #7 — Unknown `when`-clause keys silently false → validate at parse, fail loudly

**What it looks like.** T3 Code's keybinding `when` clauses use a string-expression
DSL like `"stage == swarm && agent.idle"`. Unknown identifiers (`stage == swrm`
typo) evaluate to `false` rather than raising — so a typoed binding silently
never fires.

**Why Hoopoe refuses it.** A binding that "doesn't work today and we don't know
why" is exactly the bug class §1.4 (inspectability) is designed to prevent. The
when-clause parser must hold a closed set of known context keys; an unknown
identifier raises a parse error at module-load time, the keybinding is rejected,
and a Diagnostics entry surfaces the typo with file:line.

**How CI detects it.**

```bash
bun test apps/desktop/src/renderer/keybindings/parser.test.ts
# Test fixture loads { key: 'cmd+x', command: 'foo', when: 'stage == swrm' }
# (typo) and asserts the parser raises with location info; the keybinding is
# NOT registered; a Diagnostics entry exists pointing at the typo.
```

**Implementation owner.** hp-rth (when-clause parser).

**Remedy when triggered.** Add the missing context key to the known set
intentionally (and add a test for its evaluation), or fix the typo. Never silence
the parse error.

---

## CI integration

The `antipattern-compliance` job (hp-iswv DOD) runs all seven checks above on
every PR + nightly:

1. greps for `PubSub.unbounded` + unbuffered fan-out chans without rationale (§#1)
2. asserts no PTY-attach payload exceeds 512 KiB (§#2)
3. asserts every long list either virtualizes or surfaces "showing latest N" (§#3)
4. asserts `apps/desktop/src/main.ts` < 200 lines and each `main/*.ts` < 1200 (§#4)
5. asserts BackendLifecycle uses `findOpenPort` (§#5)
6. asserts no top-level command-name switch in the main process (§#6)
7. runs the when-clause parse-error fixture (§#7)

Each failure includes a structured pointer back to this document
(`docs/antipatterns.md#N`) so the contributor sees the rationale, not just the
gate text.

## Adding new anti-patterns

If a new anti-pattern emerges post-launch (e.g., a t3code drift, a new lifted
helper that introduces a fresh trap), append it as `#8`/`#9`/etc. with the same
five-section structure: *what it looks like*, *why Hoopoe refuses it*, *how CI
detects it*, *implementation owner*, *remedy when triggered*. Never silently
expand an existing item to cover a new pattern — keep one anti-pattern per
section so CI failure messages are specific.

## Cross-references

- `plan.md` Appendix B "Anti-patterns to refuse" (lines 2331-2338) — authoritative source.
- `plan.md` Appendix C "Non-negotiable implementation guardrails" — runtime rules; this
  document covers structural rules.
- `AGENTS.md` "Hoopoe Non-Negotiable Guardrails" — companion list (12 guardrails); a
  failed grep on any guardrail is also a failed PR.
- `docs/source-provenance.md` — what was lifted, what was refused, why.
- §10.1 backpressure rules (#1) · §7.3 PTY plumbing (#2) · §1.4 inspectability (#3, #6, #7) ·
  §1.5 restartability (#5) · §1.6 boring first run (#4).
