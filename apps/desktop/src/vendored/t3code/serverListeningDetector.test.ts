// Hoopoe-owned tests for the vendored t3code helper in this directory.
// The implementation file (./serverListeningDetector.ts) carries the MIT
// notice; tests are Hoopoe-authored and not subject to the lift policy.

import { expect, test } from "bun:test";
import { ServerListeningDetector } from "./serverListeningDetector.ts";

test("serverListeningDetector: matches `Listening on http://` from stdout chunks", async () => {
  const detector = new ServerListeningDetector();
  detector.push("starting hoopoe daemon...\n");
  detector.push("Listening on http://127.0.0.1:3779\n");
  await detector.promise;
});

test("serverListeningDetector: ignores noise before signature, accepts CR-suffixed", async () => {
  const detector = new ServerListeningDetector();
  detector.push("warning: deprecated config key\r\n");
  detector.push("warning: skipping ssl\r\n");
  detector.push("Listening on http://[::1]:3779\r\n");
  await detector.promise;
});

test("serverListeningDetector: rejects via fail()", async () => {
  const detector = new ServerListeningDetector();
  const reason = new Error("backend exited prematurely");
  detector.fail(reason);
  await expect(detector.promise).rejects.toBe(reason);
});
