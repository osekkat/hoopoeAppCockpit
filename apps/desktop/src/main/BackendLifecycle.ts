// Hoopoe-owned. Adapts vendored t3code lifecycle helpers to launch Hoopoe's
// Go daemon binary (not Node-via-Electron). Decomposed from t3code's
// 2,175-line apps/desktop/src/main.ts on day one per plan.md Appendix B
// "Anti-patterns to refuse" #4.
//
// The ELECTRON_RUN_AS_NODE codepath used by t3code (running their Bun server
// from the Electron binary) is intentionally absent. Hoopoe ships a separate
// signed Go binary; trying to reuse Electron as Node here would be a footgun.

import { spawn, type ChildProcess, type SpawnOptions } from "node:child_process";
import {
  resolveDesktopBackendPort,
  type ResolveDesktopBackendPortOptions,
} from "../vendored/t3code/backendPort.ts";
import {
  waitForHttpReady,
  type WaitForHttpReadyOptions,
} from "../vendored/t3code/backendReadiness.ts";
import { ServerListeningDetector } from "../vendored/t3code/serverListeningDetector.ts";

export interface BackendSpawnOptions {
  readonly daemonBinaryPath: string;
  readonly extraArgs?: readonly string[];
  readonly host?: string;
  readonly env?: NodeJS.ProcessEnv;
  readonly portResolver?: ResolveDesktopBackendPortOptions["canListenOnHost"];
  readonly readinessOptions?: Omit<WaitForHttpReadyOptions, "signal" | "fetchImpl">;
  readonly fetchImpl?: typeof fetch;
  readonly spawnImpl?: typeof spawn;
  readonly logger?: BackendLogger;
  /** Hard ceiling on how long we wait for the daemon to print
   *  "Listening on http://...". If exceeded, we SIGKILL the child and throw —
   *  prevents indefinite UI hang when daemon is alive but wedged. */
  readonly startupTimeoutMs?: number;
}

export interface BackendLogger {
  readonly info: (message: string, meta?: Record<string, unknown>) => void;
  readonly warn: (message: string, meta?: Record<string, unknown>) => void;
  readonly error: (message: string, meta?: Record<string, unknown>) => void;
}

const noopLogger: BackendLogger = {
  info() {},
  warn() {},
  error() {},
};

export interface BackendHandle {
  readonly port: number;
  readonly baseUrl: string;
  readonly child: ChildProcess;
  readonly stop: (options?: BackendStopOptions) => Promise<void>;
}

export interface BackendStopOptions {
  /** Milliseconds to wait after SIGTERM before escalating to SIGKILL. */
  readonly graceMs?: number;
}

const DEFAULT_GRACE_MS = 8_000;
const DEFAULT_STARTUP_TIMEOUT_MS = 60_000;

// Tracks child processes whose exit was explicitly requested by `stop()`.
// Lets the surrounding code distinguish a clean shutdown from a crash without
// stashing flags on the ChildProcess instance itself.
const expectedBackendExitChildren = new WeakSet<ChildProcess>();

/**
 * Find an open TCP port starting from `preferred`. Closes the t3code Appendix B
 * #5 anti-pattern (no port-conflict resolution) by always probing upward and
 * verifying each candidate against the daemon's bind hosts before returning.
 */
export async function findOpenPort(
  preferred: number,
  options: Omit<ResolveDesktopBackendPortOptions, "startPort"> = { host: "127.0.0.1" },
): Promise<number> {
  return await resolveDesktopBackendPort({
    ...options,
    startPort: preferred,
  });
}

/** Spawn the Hoopoe Go daemon, wait for it to log "Listening on http://", and
 * verify HTTP readiness on the resolved port. Returns a handle that owns the
 * child process and exposes a `stop()` that escalates SIGTERM → SIGKILL. */
export async function spawnBackend(options: BackendSpawnOptions): Promise<BackendHandle> {
  const logger = options.logger ?? noopLogger;
  const host = options.host ?? "127.0.0.1";
  const spawnImpl = options.spawnImpl ?? spawn;
  const fetchImpl = options.fetchImpl ?? fetch;

  const port = await findOpenPort(3779, {
    host,
    ...(options.portResolver ? { canListenOnHost: options.portResolver } : {}),
  });
  const baseUrl = `http://${host}:${port}`;

  const args = [
    "--host",
    host,
    "--port",
    String(port),
    ...(options.extraArgs ?? []),
  ];
  const spawnOptions: SpawnOptions = {
    stdio: ["ignore", "pipe", "pipe"],
    ...(options.env ? { env: options.env } : {}),
  };
  const child = spawnImpl(options.daemonBinaryPath, args, spawnOptions);
  logger.info("backend.spawn", { binary: options.daemonBinaryPath, port, host });

  const detector = new ServerListeningDetector();
  child.stdout?.on("data", (chunk) => {
    detector.push(chunk);
  });
  child.stderr?.on("data", (chunk) => {
    detector.push(chunk);
  });

  const earlyExit = new Promise<never>((_, reject) => {
    child.once("exit", (code, signal) => {
      const expected = expectedBackendExitChildren.has(child);
      if (!expected) {
        const reason = `daemon exited before listening (code=${code} signal=${signal ?? "none"})`;
        logger.error("backend.early-exit", { code, signal: signal ?? null });
        detector.fail(new Error(reason));
        reject(new Error(reason));
      }
    });
  });

  // Hard cap on the listening-detector wait. Without it, a daemon that's
  // alive but wedged (no log line, no exit) hangs `Promise.race` forever
  // and the desktop boot screen never resolves.
  const startupTimeoutMs = options.startupTimeoutMs ?? DEFAULT_STARTUP_TIMEOUT_MS;
  let startupTimer: NodeJS.Timeout | null = null;
  const startupDeadline = new Promise<never>((_, reject) => {
    startupTimer = setTimeout(() => {
      const reason = `daemon did not log "Listening on http://" within ${startupTimeoutMs}ms`;
      detector.fail(new Error(reason));
      reject(new Error(reason));
    }, startupTimeoutMs);
  });

  try {
    await Promise.race([detector.promise, earlyExit, startupDeadline]);
    await waitForHttpReady(baseUrl, {
      ...options.readinessOptions,
      fetchImpl,
    });
  } catch (error) {
    expectedBackendExitChildren.add(child);
    if (!child.killed) {
      child.kill("SIGKILL");
    }
    throw error;
  } finally {
    if (startupTimer !== null) clearTimeout(startupTimer);
  }

  logger.info("backend.ready", { baseUrl });

  return {
    port,
    baseUrl,
    child,
    stop: async (stopOptions = {}) => {
      await stopBackendAndWaitForExit(child, stopOptions, logger);
    },
  };
}

export async function stopBackendAndWaitForExit(
  child: ChildProcess,
  options: BackendStopOptions,
  logger: BackendLogger = noopLogger,
): Promise<void> {
  if (child.exitCode !== null) return;
  expectedBackendExitChildren.add(child);
  const graceMs = options.graceMs ?? DEFAULT_GRACE_MS;
  logger.info("backend.stop", { graceMs });

  return await new Promise<void>((resolve) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      clearTimeout(killTimer);
      resolve();
    };
    child.once("exit", finish);
    child.kill("SIGTERM");
    const killTimer = setTimeout(() => {
      if (child.exitCode === null) {
        logger.warn("backend.stop.escalate-sigkill", {});
        child.kill("SIGKILL");
      }
    }, graceMs);
  });
}

/** Test-only export: lets unit tests assert the WeakSet semantics without
 * exposing the WeakSet itself in the module's public API. */
export const internalsForTesting = {
  isExpectedExit(child: ChildProcess): boolean {
    return expectedBackendExitChildren.has(child);
  },
};
