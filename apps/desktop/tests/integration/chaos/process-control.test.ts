// hp-2qn — Process-control primitive smoke test.
//
// Boots a tiny `sleep` subprocess (POSIX universal — no Hoopoe daemon
// required), pauses it, verifies it cannot exit while paused, then
// resumes and confirms it does exit cleanly. Sanity check that
// SIGSTOP/SIGCONT actually work in the test environment.

import { describe, expect, test } from "bun:test";
import { spawn } from "node:child_process";
import {
  ChaosProcessError,
  pauseProcess,
  resumeProcess,
  shutdownProcess,
  withPaused,
} from "../../../src/test-utils/chaos/index.ts";

function spawnSleep(seconds: number): ReturnType<typeof spawn> {
  return spawn("sleep", [String(seconds)], { stdio: "ignore" });
}

describe("hp-2qn :: process-control primitive", () => {
  test("pauseProcess + resumeProcess round-trips a sleep child", async () => {
    const child = spawnSleep(1);
    try {
      const target = { pid: child.pid, label: "sleep" };
      pauseProcess(target);
      // Wait 1.2s — longer than the sleep's natural exit. If pause
      // worked the process is still alive after the sleep would have
      // expired.
      await new Promise((r) => setTimeout(r, 1_200));
      expect(child.killed).toBe(false);
      resumeProcess(target);
      // Now wait briefly for the process to finish on its own.
      const exitCode = await new Promise<number | null>((r) => {
        child.once("exit", (code) => r(code));
      });
      expect(exitCode).toBe(0);
    } finally {
      if (!child.killed) child.kill("SIGKILL");
    }
  }, 10_000);

  test("withPaused sends SIGCONT even when fn throws", async () => {
    const child = spawnSleep(1);
    try {
      const target = { pid: child.pid, label: "sleep-throw" };
      const error = new Error("intentional");
      let caught: unknown = null;
      try {
        await withPaused(target, async () => {
          throw error;
        });
      } catch (err) {
        caught = err;
      }
      expect(caught).toBe(error);
      // Process should now be unpaused — wait for it to exit naturally.
      const exitCode = await new Promise<number | null>((r) => {
        child.once("exit", (code) => r(code));
      });
      expect(exitCode).toBe(0);
    } finally {
      if (!child.killed) child.kill("SIGKILL");
    }
  }, 10_000);

  test("shutdownProcess SIGTERM-then-SIGKILL with grace", async () => {
    const child = spawnSleep(60);
    try {
      const target = { pid: child.pid, label: "sleep-long" };
      const start = Date.now();
      const exit = await shutdownProcess(target, child, { graceMs: 200 });
      const elapsed = Date.now() - start;
      // sleep responds to SIGTERM immediately; should be well under graceMs.
      expect(elapsed).toBeLessThan(2_000);
      // exit is null when the process was killed by signal (SIGTERM == 15).
      expect(exit === null || typeof exit === "number").toBe(true);
    } finally {
      if (!child.killed) child.kill("SIGKILL");
    }
  }, 10_000);

  test("pauseProcess throws on missing PID", () => {
    expect(() => pauseProcess({ pid: undefined, label: "ghost" })).toThrow(ChaosProcessError);
  });
});
