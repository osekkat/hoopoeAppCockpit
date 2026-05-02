// Hoopoe-owned tests for the vendored t3code helper in this directory.
// The implementation file (./runtimeArch.ts) carries the MIT notice;
// tests are Hoopoe-authored and not subject to the lift policy.

import { expect, test } from "bun:test";
import {
  isArm64HostRunningIntelBuild,
  resolveDesktopRuntimeInfo,
} from "./runtimeArch.ts";

test("runtimeArch: native arm64 build on arm64 macOS host", () => {
  const info = resolveDesktopRuntimeInfo({
    platform: "darwin",
    processArch: "arm64",
    runningUnderArm64Translation: false,
  });
  expect(info).toEqual({
    hostArch: "arm64",
    appArch: "arm64",
    runningUnderArm64Translation: false,
  });
  expect(isArm64HostRunningIntelBuild(info)).toBe(false);
});

test("runtimeArch: x64 build under Rosetta on arm64 macOS host", () => {
  const info = resolveDesktopRuntimeInfo({
    platform: "darwin",
    processArch: "x64",
    runningUnderArm64Translation: true,
  });
  expect(info).toEqual({
    hostArch: "arm64",
    appArch: "x64",
    runningUnderArm64Translation: true,
  });
  expect(isArm64HostRunningIntelBuild(info)).toBe(true);
});

test("runtimeArch: native x64 build on x64 macOS host", () => {
  const info = resolveDesktopRuntimeInfo({
    platform: "darwin",
    processArch: "x64",
    runningUnderArm64Translation: false,
  });
  expect(info).toEqual({
    hostArch: "x64",
    appArch: "x64",
    runningUnderArm64Translation: false,
  });
  expect(isArm64HostRunningIntelBuild(info)).toBe(false);
});

test("runtimeArch: non-darwin platform reports identical host/app arch", () => {
  const info = resolveDesktopRuntimeInfo({
    platform: "linux",
    processArch: "x64",
    runningUnderArm64Translation: false,
  });
  expect(info.hostArch).toBe("x64");
  expect(info.appArch).toBe("x64");
  expect(info.runningUnderArm64Translation).toBe(false);
});
