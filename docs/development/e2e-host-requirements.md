# E2E Host Requirements (hp-411d)

The Playwright suites under `apps/desktop/tests/{smoke,e2e}/` drive a real Chromium browser — Playwright's bundled binary, not the system one. Chromium dynamically links against several X11 / GBM / GTK libraries that aren't always installed on barebones Linux hosts (CI runners, fresh dev VMs, headless containers).

`bun run e2e` at the repo root invokes `scripts/e2e/run-e2e.ts`, which:

1. Detects host readiness via `detectChromiumHost()` (probes `libgbm.so.1` on Linux; macOS / Windows are always ready).
2. **Ready:** runs the smoke suite, then the hp-j30 suite. Aggregate exit code surfaces to the shell.
3. **Not ready:** prints structured `e2e.suite.skipped` envelopes for both suites with the same reason and exits `0`. CI on a barebones host stays green; humans see the install hint.

The same helper backs `test.skip()` in both spec files (`apps/desktop/tests/smoke/e2e/desktop-shell.spec.ts` + `apps/desktop/tests/e2e/hp-j30-desktop-shell.spec.ts`) so individual suites can be invoked directly without diverging from the orchestrator's verdict.

## Required system packages (Linux)

On Debian/Ubuntu hosts, install:

```sh
sudo apt-get update
sudo apt-get install -y \
  libgbm1 \
  libgtk-3-0 \
  libnss3 \
  libasound2t64 \
  libdrm2 \
  libxcomposite1 \
  libxdamage1 \
  libxfixes3 \
  libxrandr2 \
  libxkbcommon0 \
  libpango-1.0-0 \
  libcairo2 \
  fonts-liberation \
  xvfb
```

Or use Playwright's first-party installer, which keeps the package list current:

```sh
bunx playwright install --with-deps chromium
```

`--with-deps` requires `sudo` — the installer runs `apt-get install` for you. On hosts where you can't `sudo`, run `bunx playwright install chromium` (browser only, no system packages) and rely on the host already having the libs.

`xvfb` isn't strictly required (Playwright's Chromium runs headless by default), but it's listed for parity with hosts that prefer `xvfb-run` for stability under tight resource limits.

## Headless vs. headed

The default invocation (`bun run e2e`) runs headless. To debug interactively against the dev server:

```sh
# Terminal 1 — start the renderer dev server
bun run --cwd apps/desktop dev

# Terminal 2 — run smoke suite headed against the existing server
bunx playwright test -c playwright.config.ts --headed
```

`reuseExistingServer: !process.env.CI` in `playwright.config.ts` reuses the dev server on dev hosts and always starts a fresh one in CI.

## CI integration

`.github/workflows/ci.yml` runs `bun run typecheck`, `bun run test`, and `bun run e2e` on Ubuntu — installing the apt packages above before the e2e step. The runner doesn't `sudo` — it uses the GitHub-hosted image which already runs as a privileged user via `actions/runner` so direct `apt-get` works.

If you're adding a new e2e suite:

- Place it under `apps/desktop/tests/{smoke,e2e}/`.
- Begin the test file with `import { chromiumHostStatus } from "../../src/test-utils/index.ts"` and call `test.skip(!hostStatus.ready, hostStatus.reason)` at the top of `test.describe`. This keeps the orchestrator's pass/skip decision aligned with the per-suite decision, so the developer never sees one suite failing while another skips.
- Add the suite to `SUITES` in `scripts/e2e/run-e2e.ts` so the root runner picks it up.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `error while loading shared libraries: libgbm.so.1` | Host missing `libgbm1` | `apt-get install libgbm1` or `bunx playwright install --with-deps chromium` |
| Tests pass locally, fail in CI with "Browser closed unexpectedly" | OOM under low CI memory | Cap workers to 1 in `playwright.config.ts` (already the case for hp-411d) |
| `Error: ENOSPC: System limit for number of file watchers reached` | Linux inotify limit | `echo fs.inotify.max_user_watches=524288 \| sudo tee -a /etc/sysctl.conf && sudo sysctl -p` |
| Suite skips on a host that DOES have `libgbm1` | Probe path doesn't match the system | Add the path to `LINUX_CHROMIUM_LIBS` in `apps/desktop/src/test-utils/chromium-host.ts` |
