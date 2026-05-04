// hp-ngq — TS-side daemon-spawn harness for integration tests.
//
// Re-exports the idempotency contract surface so test files only
// have to import from one path. The contract table itself lives in
// a sibling module to keep the harness file focused on subprocess
// management.
export {
  IDEMPOTENCY_HEADER,
  IDEMPOTENT_WRITE_ENDPOINTS,
  makeIdempotencyKey,
  type IdempotentWriteEndpoint,
} from "./idempotency-contract.ts";


//
// Spawns the Go daemon binary (`apps/daemon/bin/hoopoed`, defaulting to
// `--mock` mode for deterministic fixture-backed runs), waits for
// `/v1/health` to return 200, and yields a `DaemonHandle` with `baseUrl`
// + `kill()` for tests to drive HTTP / WS endpoints.
//
// Why this lives here (not in a shared package): only the integration
// suite under `apps/desktop/tests/integration/**` consumes it today;
// keeping it co-located with its consumers avoids an extra workspace
// dep until a second consumer materializes.
//
// The harness is defensive about missing/unbuilt daemon binaries: it
// returns `{ok: false, reason: '...'}` from `tryStart()` so the test
// suite can `it.skip()` gracefully on hosts where the daemon hasn't
// been built (e.g. CI lanes that only test the desktop side).

import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { createServer } from "node:net";
import { resolve } from "node:path";

export interface DaemonStartOptions {
  /** Mode to start in. `mock` boots the fixture-backed daemon; `real`
   *  boots the production transport. Default: `mock`. */
  mode?: "mock" | "real";
  /** Override the binary path. Default:
   *  `<repoRoot>/apps/daemon/bin/hoopoed`. */
  binaryPath?: string;
  /** Repo root used to resolve the binary. Default: `process.cwd()`. */
  repoRoot?: string;
  /** Override the port. If omitted, a free port is allocated. */
  port?: number;
  /** Override `HOOPOE_HOME` (the daemon's data directory). Default: a
   *  temp directory inside `os.tmpdir()`. */
  daemonHome?: string;
  /** Override `HOOPOE_AUTH_TOKEN`. Default: a stable mock token so the
   *  bearer-protected routes accept requests in tests. */
  authToken?: string;
  /** How long to wait for `/v1/health` to return 200. Default: 5_000ms. */
  readyTimeoutMs?: number;
  /** Polling interval while waiting. Default: 50ms. */
  readyPollMs?: number;
  /** Override `process.env` passthrough (defaults to inheriting). */
  envOverrides?: Record<string, string>;
}

export interface DaemonHandle {
  /** `http://127.0.0.1:<port>` — base URL for HTTP requests. */
  baseUrl: string;
  /** Allocated/declared port. */
  port: number;
  /** Auth token to include in `Authorization: Bearer <token>` headers. */
  authToken: string;
  /** Resolved `HOOPOE_HOME`. */
  daemonHome: string;
  /** Spawned subprocess (exposed for advanced control / forensics). */
  process: ChildProcess;
  /** Promise that resolves when the daemon emits `/v1/health: ok`. */
  ready: Promise<void>;
  /** Stop the daemon. Idempotent. */
  kill: () => Promise<void>;
}

export type StartResult =
  | { ok: true; handle: DaemonHandle }
  | { ok: false; reason: string };

const DEFAULT_AUTH_TOKEN = "harness-hp-ngq-mock-token";

function defaultBinaryPath(repoRoot: string): string {
  return resolve(repoRoot, "apps", "daemon", "bin", "hoopoed");
}

async function pickFreePort(): Promise<number> {
  return await new Promise<number>((resolveFn, rejectFn) => {
    const server = createServer();
    server.unref();
    server.on("error", rejectFn);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (address === null || typeof address === "string") {
        server.close();
        rejectFn(new Error("could not allocate ephemeral port"));
        return;
      }
      const port = address.port;
      server.close(() => resolveFn(port));
    });
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

interface PollResult {
  ok: boolean;
  /** Last error encountered while polling, if any. */
  lastError?: string;
}

async function pollForReady(baseUrl: string, timeoutMs: number, intervalMs: number): Promise<PollResult> {
  const deadline = Date.now() + timeoutMs;
  let lastError: string | undefined;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseUrl}/v1/health`, {
        signal: AbortSignal.timeout(Math.max(250, intervalMs * 4)),
      });
      if (res.status === 200) return { ok: true };
      lastError = `unexpected status ${res.status}`;
    } catch (err) {
      lastError = (err as Error).message;
    }
    await sleep(intervalMs);
  }
  return { ok: false, lastError: lastError ?? "timed out polling /v1/health" };
}

function makeArgs(options: Required<Pick<DaemonStartOptions, "mode">>): string[] {
  const args: string[] = [];
  if (options.mode === "mock") args.push("--mock");
  return args;
}

/** Try to start the daemon. Returns `{ok: true}` on success or
 *  `{ok: false, reason}` if the binary is missing / failed to start /
 *  never reported ready. Tests should `it.skip()` on `ok: false`. */
export async function tryStartDaemon(
  options: DaemonStartOptions = {},
): Promise<StartResult> {
  const repoRoot = options.repoRoot ?? process.cwd();
  const binaryPath = options.binaryPath ?? defaultBinaryPath(repoRoot);
  if (!existsSync(binaryPath)) {
    return { ok: false, reason: `daemon binary not found at ${binaryPath} — run \`bun run --cwd apps/daemon build\`` };
  }
  const mode = options.mode ?? "mock";
  const port = options.port ?? (await pickFreePort());
  const baseUrl = `http://127.0.0.1:${port}`;
  const authToken = options.authToken ?? DEFAULT_AUTH_TOKEN;
  const daemonHome =
    options.daemonHome ?? resolve("/tmp", `hoopoe-harness-${process.pid}-${port}`);

  const env: NodeJS.ProcessEnv = {
    ...process.env,
    HOOPOE_PORT: String(port),
    HOOPOE_HOST: "127.0.0.1",
    HOOPOE_HOME: daemonHome,
    HOOPOE_AUTH_TOKEN: authToken,
    ...(options.envOverrides ?? {}),
  };

  const child = spawn(binaryPath, makeArgs({ mode }), {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
    env,
    detached: false,
  });

  let exited = false;
  let exitInfo: string | null = null;
  child.once("exit", (code, signal) => {
    exited = true;
    exitInfo = `exited (code=${code}, signal=${signal})`;
  });
  child.once("error", (err) => {
    exited = true;
    exitInfo = `spawn error: ${(err as Error).message}`;
  });

  const ready = pollForReady(baseUrl, options.readyTimeoutMs ?? 5_000, options.readyPollMs ?? 50);

  const result = await ready;
  if (!result.ok || exited) {
    try {
      child.kill("SIGKILL");
    } catch {
      // ignore
    }
    return {
      ok: false,
      reason: exitInfo ?? result.lastError ?? "daemon did not become ready",
    };
  }

  let killed = false;
  const handle: DaemonHandle = {
    baseUrl,
    port,
    authToken,
    daemonHome,
    process: child,
    ready: Promise.resolve(),
    kill: async () => {
      if (killed) return;
      killed = true;
      if (!exited) {
        child.kill("SIGTERM");
        // Give it 500ms to exit cleanly, then SIGKILL.
        const exitedCleanly = await Promise.race([
          new Promise<boolean>((resolveFn) => child.once("exit", () => resolveFn(true))),
          sleep(500).then(() => false),
        ]);
        if (!exitedCleanly) {
          try {
            child.kill("SIGKILL");
          } catch {
            // ignore
          }
        }
      }
    },
  };
  return { ok: true, handle };
}

/** Convenience wrapper: throw on failure (for tests that *require* the
 *  daemon to be available — a missing binary means the suite as a whole
 *  cannot run). Most tests should prefer `tryStartDaemon` + `it.skip`. */
export async function startDaemonOrThrow(options: DaemonStartOptions = {}): Promise<DaemonHandle> {
  const result = await tryStartDaemon(options);
  if (!result.ok) {
    throw new Error(`daemon-harness: failed to start — ${result.reason}`);
  }
  return result.handle;
}
