// hp-2n1 — disk size + cap evaluation tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  DEFAULT_CLONE_CAPS,
  directorySizeBytes,
  evaluateCaps,
  validateCaps,
  type CloneCapConfig,
} from "./index.ts";

let tempRoot: string;

beforeEach(() => {
  tempRoot = mkdtempSync(join(tmpdir(), "hoopoe-clone-disk-"));
});

afterEach(() => {
  rmSync(tempRoot, { recursive: true, force: true });
});

test("directorySizeBytes: returns 0 for missing root", () => {
  expect(directorySizeBytes(join(tempRoot, "nope"))).toBe(0);
});

test("directorySizeBytes: returns 0 for empty directory", () => {
  expect(directorySizeBytes(tempRoot)).toBe(0);
});

test("directorySizeBytes: sums sizes across nested directories", () => {
  writeFileSync(join(tempRoot, "a.txt"), "AAA");
  mkdirSync(join(tempRoot, "nested"));
  writeFileSync(join(tempRoot, "nested", "b.txt"), "BBBB");
  writeFileSync(join(tempRoot, "nested", "c.txt"), "CCCCC");
  mkdirSync(join(tempRoot, "nested", "deep"));
  writeFileSync(join(tempRoot, "nested", "deep", "d.txt"), "DD");
  expect(directorySizeBytes(tempRoot)).toBe(3 + 4 + 5 + 2);
});

test("directorySizeBytes: skips symlinks (no infinite recursion / outbound size)", () => {
  writeFileSync(join(tempRoot, "real.txt"), "12345");
  // Symlink to /etc — its content shouldn't be counted, and we shouldn't
  // crash following it.
  try {
    symlinkSync("/etc", join(tempRoot, "link-to-etc"));
  } catch {
    return; // Filesystem doesn't support symlinks (rare on Linux); skip.
  }
  expect(directorySizeBytes(tempRoot)).toBe(5);
});

test("evaluateCaps: ok verdict when below soft cap", () => {
  const out = evaluateCaps(1024 * 1024); // 1 MB
  expect(out.verdict).toBe("ok");
  expect(out.fetchAllowed).toBe(true);
  expect(out.excessBytes).toBe(0);
  expect(out.caps).toBe(DEFAULT_CLONE_CAPS);
});

test("evaluateCaps: soft cap exceeded — fetch still allowed", () => {
  const size = DEFAULT_CLONE_CAPS.softCapBytes + 100;
  const out = evaluateCaps(size);
  expect(out.verdict).toBe("soft_cap_exceeded");
  expect(out.fetchAllowed).toBe(true);
  expect(out.excessBytes).toBe(100);
});

test("evaluateCaps: hard cap exceeded — fetch refused", () => {
  const size = DEFAULT_CLONE_CAPS.hardCapBytes + 1;
  const out = evaluateCaps(size);
  expect(out.verdict).toBe("hard_cap_exceeded");
  expect(out.fetchAllowed).toBe(false);
  expect(out.excessBytes).toBe(1);
});

test("evaluateCaps: per-project override is honored", () => {
  const tiny: CloneCapConfig = { softCapBytes: 100, hardCapBytes: 200 };
  expect(evaluateCaps(50, tiny).verdict).toBe("ok");
  expect(evaluateCaps(150, tiny).verdict).toBe("soft_cap_exceeded");
  expect(evaluateCaps(250, tiny).verdict).toBe("hard_cap_exceeded");
});

test("validateCaps: refuses non-positive sizes", () => {
  expect(() => validateCaps({ softCapBytes: 0, hardCapBytes: 1 })).toThrow(/invalid_caps/);
  expect(() => validateCaps({ softCapBytes: -1, hardCapBytes: 1 })).toThrow(/invalid_caps/);
});

test("validateCaps: refuses hard < soft", () => {
  expect(() => validateCaps({ softCapBytes: 200, hardCapBytes: 100 })).toThrow(/invalid_caps/);
});

test("DEFAULT_CLONE_CAPS: 2 GB soft / 5 GB hard per plan.md §7.7", () => {
  expect(DEFAULT_CLONE_CAPS.softCapBytes).toBe(2 * 1024 * 1024 * 1024);
  expect(DEFAULT_CLONE_CAPS.hardCapBytes).toBe(5 * 1024 * 1024 * 1024);
});
