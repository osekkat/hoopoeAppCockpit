import { expect, test } from "bun:test";
import {
  detectChromiumHost,
  resetChromiumHostStatusForTesting,
  chromiumHostStatus,
} from "./chromium-host.ts";

test("detectChromiumHost: non-Linux platforms always ready", () => {
  for (const platform of ["darwin", "win32", "freebsd"] as const) {
    const status = detectChromiumHost({
      platform,
      fileExistsImpl: () => false,
    });
    expect(status.ready).toBe(true);
    expect(status.platform).toBe(platform);
  }
});

test("detectChromiumHost: Linux ready when at least one libgbm path exists", () => {
  const status = detectChromiumHost({
    platform: "linux",
    fileExistsImpl: (path) => path === "/usr/lib/x86_64-linux-gnu/libgbm.so.1",
  });
  expect(status.ready).toBe(true);
  expect(status.reason).toContain("libgbm.so.1 present");
});

test("detectChromiumHost: Linux not ready when every probe path is missing", () => {
  const status = detectChromiumHost({
    platform: "linux",
    fileExistsImpl: () => false,
  });
  expect(status.ready).toBe(false);
  expect(status.reason).toContain("libgbm.so.1 not found");
  expect(status.reason).toContain("playwright install-deps");
  expect(status.reason).toContain("e2e-host-requirements.md");
});

test("detectChromiumHost: aarch64 path is recognized", () => {
  const status = detectChromiumHost({
    platform: "linux",
    fileExistsImpl: (path) => path === "/usr/lib/aarch64-linux-gnu/libgbm.so.1",
  });
  expect(status.ready).toBe(true);
});

test("chromiumHostStatus: caches across calls", () => {
  resetChromiumHostStatusForTesting();
  const a = chromiumHostStatus();
  const b = chromiumHostStatus();
  expect(a).toBe(b);
});
