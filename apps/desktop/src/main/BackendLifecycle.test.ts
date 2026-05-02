import { EventEmitter } from "node:events";
import type { ChildProcess, SpawnOptions } from "node:child_process";
import { Readable } from "node:stream";
import { expect, test } from "bun:test";
import {
  internalsForTesting,
  findOpenPort,
  spawnBackend,
  stopBackendAndWaitForExit,
} from "./BackendLifecycle.ts";

class FakeChild extends EventEmitter {
  readonly stdout = new Readable({ read() {} });
  readonly stderr = new Readable({ read() {} });
  killed = false;
  exitCode: number | null = null;
  kill(signal?: NodeJS.Signals | number): boolean {
    this.killed = true;
    setImmediate(() => {
      this.exitCode = 0;
      this.emit("exit", 0, signal ?? null);
    });
    return true;
  }
}

function fakeSpawn(): {
  readonly child: FakeChild;
  readonly impl: (
    command: string,
    args: readonly string[],
    options: SpawnOptions,
  ) => ChildProcess;
} {
  const child = new FakeChild();
  return {
    child,
    impl: () => child as unknown as ChildProcess,
  };
}

test("findOpenPort: probes upward and returns the first listenable port", async () => {
  const visited: number[] = [];
  const port = await findOpenPort(40000, {
    host: "127.0.0.1",
    canListenOnHost: async (candidate) => {
      visited.push(candidate);
      return candidate === 40002;
    },
  });
  expect(port).toBe(40002);
  expect(visited).toEqual([40000, 40001, 40002]);
});

test("spawnBackend: emits ready once stdout reports `Listening on http://` and HTTP probe succeeds", async () => {
  const { child, impl } = fakeSpawn();
  const fetchCalls: string[] = [];
  const fetchImpl = (input: string | URL): Promise<Response> => {
    fetchCalls.push(String(input));
    return Promise.resolve(new Response("ok", { status: 200 }));
  };
  setImmediate(() => {
    child.stdout.push("Listening on http://127.0.0.1:40010\n");
  });
  const handle = await spawnBackend({
    daemonBinaryPath: "/fake/hoopoe",
    spawnImpl: impl as unknown as typeof import("node:child_process").spawn,
    fetchImpl: fetchImpl as unknown as typeof fetch,
    portResolver: async (candidate, host) => candidate === 40010 && host === "127.0.0.1",
    readinessOptions: { intervalMs: 5, requestTimeoutMs: 50, timeoutMs: 1_000 },
  });
  expect(handle.port).toBe(40010);
  expect(handle.baseUrl).toBe("http://127.0.0.1:40010");
  expect(fetchCalls).toHaveLength(1);
  expect(fetchCalls[0]).toContain("http://127.0.0.1:40010");
  await handle.stop({ graceMs: 100 });
  expect(child.killed).toBe(true);
  expect(internalsForTesting.isExpectedExit(child as unknown as ChildProcess)).toBe(true);
});

test("stopBackendAndWaitForExit: marks the child as expected-exit and resolves on close", async () => {
  const { child } = fakeSpawn();
  await stopBackendAndWaitForExit(child as unknown as ChildProcess, { graceMs: 50 });
  expect(child.killed).toBe(true);
  expect(internalsForTesting.isExpectedExit(child as unknown as ChildProcess)).toBe(true);
});
