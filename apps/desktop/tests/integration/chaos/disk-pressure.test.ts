// hp-2qn — Disk-pressure primitive smoke test.
//
// Verifies the safety guards (allow-listed dirs, hard-ceiling refusal,
// idempotent release) and that the primitive actually writes the
// requested bytes inside an allow-listed temp dir. Daemon-side
// disk-pressure response (graceful tagging, audit retention) is owned
// by daemon panes and lives in their chaos suite.

import { describe, expect, test } from "bun:test";
import { mkdtempSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  ChaosDiskPressureError,
  fillDisk,
} from "../../../src/test-utils/chaos/index.ts";

describe("hp-2qn :: disk-pressure primitive", () => {
  test("writes the requested bytes inside an allow-listed temp dir", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-chaos-disk-"));
    const handle = fillDisk({ dir, totalBytes: 256 * 1024, perFileBytes: 64 * 1024 });
    try {
      expect(handle.bytesWritten).toBe(256 * 1024);
      expect(handle.paths.length).toBe(4);
      let total = 0;
      for (const path of handle.paths) {
        total += statSync(path).size;
      }
      expect(total).toBe(256 * 1024);
    } finally {
      handle.release();
    }
  });

  test("release() deletes every file written and is idempotent", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-chaos-disk-"));
    const handle = fillDisk({ dir, totalBytes: 32 * 1024, perFileBytes: 16 * 1024 });
    expect(handle.paths.length).toBe(2);
    handle.release();
    handle.release();
    for (const path of handle.paths) {
      let stillThere = false;
      try {
        statSync(path);
        stillThere = true;
      } catch {
        // expected — file is gone
      }
      expect(stillThere).toBe(false);
    }
  });

  test("refuses to write outside the allow-listed prefixes", () => {
    expect(() => fillDisk({ dir: "/etc/hoopoe-chaos", totalBytes: 1024 })).toThrow(
      ChaosDiskPressureError,
    );
    expect(() => fillDisk({ dir: "/usr/local/hoopoe-chaos", totalBytes: 1024 })).toThrow(
      ChaosDiskPressureError,
    );
  });

  test("refuses sizes above the hard ceiling unless explicitly allowed", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-chaos-disk-"));
    const oversize = 2 * 1024 * 1024 * 1024; // 2 GiB > 1 GiB ceiling
    expect(() => fillDisk({ dir, totalBytes: oversize })).toThrow(ChaosDiskPressureError);
  });

  test("rejects non-positive totalBytes", () => {
    const dir = mkdtempSync(join(tmpdir(), "hoopoe-chaos-disk-"));
    expect(() => fillDisk({ dir, totalBytes: 0 })).toThrow(ChaosDiskPressureError);
    expect(() => fillDisk({ dir, totalBytes: -1 })).toThrow(ChaosDiskPressureError);
  });
});
