// hp-2qn — Process-control chaos primitives.
//
// Wrap signal-based fault injection (`SIGSTOP` / `SIGCONT` / `SIGTERM`
// / `SIGKILL`) so chaos tests can describe their intent ("freeze the
// daemon for 2s, then resume") without re-implementing the signal
// dance per test. All primitives operate on the `DaemonHandle` /
// `ChildProcess` returned by the daemon-spawn harness from hp-ngq.

import type { ChildProcess } from "node:child_process";

export class ChaosProcessError extends Error {
  override readonly name = "ChaosProcessError";
  constructor(message: string) {
    super(message);
  }
}

export interface ProcessTarget {
  /** PID of the target process. Negative values target the process
   *  group instead. */
  pid: number | undefined;
  /** Optional human-readable label used in error messages. */
  label?: string;
}

function ensurePid(target: ProcessTarget, action: string): number {
  if (target.pid === undefined) {
    throw new ChaosProcessError(
      `${action}: target ${target.label ?? "process"} has no PID — was it already terminated?`,
    );
  }
  return target.pid;
}

/** Send SIGSTOP — pauses the target until SIGCONT. Idempotent: a
 *  second pause on an already-stopped process is a no-op (POSIX
 *  semantics). */
export function pauseProcess(target: ProcessTarget): void {
  const pid = ensurePid(target, "pauseProcess");
  try {
    process.kill(pid, "SIGSTOP");
  } catch (err) {
    throw new ChaosProcessError(
      `pauseProcess(${target.label ?? pid}): SIGSTOP failed: ${(err as Error).message}`,
    );
  }
}

/** Send SIGCONT — resumes a paused target. Idempotent: a SIGCONT to a
 *  running process is a no-op. */
export function resumeProcess(target: ProcessTarget): void {
  const pid = ensurePid(target, "resumeProcess");
  try {
    process.kill(pid, "SIGCONT");
  } catch (err) {
    throw new ChaosProcessError(
      `resumeProcess(${target.label ?? pid}): SIGCONT failed: ${(err as Error).message}`,
    );
  }
}

/** Bracket a chaos action: pause → run callback → resume. Even if the
 *  callback throws, SIGCONT is still sent so the harness can't leave
 *  a permanently-frozen daemon behind. */
export async function withPaused<T>(
  target: ProcessTarget,
  fn: () => Promise<T>,
): Promise<T> {
  pauseProcess(target);
  try {
    return await fn();
  } finally {
    try {
      resumeProcess(target);
    } catch {
      // best effort
    }
  }
}

export interface RestartOptions {
  /** Grace window between SIGTERM and SIGKILL. Default 500ms. */
  graceMs?: number;
}

/** Send SIGTERM and wait for the process to exit. Falls back to
 *  SIGKILL after `graceMs`. Returns the exit code (or null on signal
 *  termination). The caller is responsible for re-spawning after this
 *  completes — `restartDaemon` in the daemon-harness wraps this with
 *  a fresh `tryStartDaemon`. */
export async function shutdownProcess(
  target: ProcessTarget,
  child: ChildProcess,
  options: RestartOptions = {},
): Promise<number | null> {
  const pid = ensurePid(target, "shutdownProcess");
  const graceMs = options.graceMs ?? 500;
  const exitPromise = new Promise<number | null>((resolveFn) => {
    child.once("exit", (code) => resolveFn(code));
  });
  try {
    process.kill(pid, "SIGTERM");
  } catch (err) {
    throw new ChaosProcessError(
      `shutdownProcess(${target.label ?? pid}): SIGTERM failed: ${(err as Error).message}`,
    );
  }
  const winner = await Promise.race<number | null | "timeout">([
    exitPromise,
    new Promise<"timeout">((r) => setTimeout(() => r("timeout"), graceMs)),
  ]);
  if (winner === "timeout") {
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // already gone
    }
    return await exitPromise;
  }
  return winner;
}
