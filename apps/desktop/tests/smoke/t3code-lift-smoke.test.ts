import { expect, test } from "bun:test";
import { spawn } from "node:child_process";
import * as FS from "node:fs";
import * as Path from "node:path";
import * as OS from "node:os";
import { fileURLToPath } from "node:url";
import {
  readSavedEnvironmentSecret,
  writeSavedEnvironmentRegistry,
  writeSavedEnvironmentSecret,
  type DesktopSecretStorage,
} from "../../src/vendored/t3code/clientPersistence.ts";
import { ServerListeningDetector } from "../../src/vendored/t3code/serverListeningDetector.ts";
import {
  isArm64HostRunningIntelBuild,
  resolveDesktopRuntimeInfo,
} from "../../src/vendored/t3code/runtimeArch.ts";
import { CommandRegistry } from "../../src/main/CommandRegistry.ts";
import { DEFAULT_KEYBINDINGS, KeybindingsManager } from "../../src/main/keybindings/index.ts";

const smokeDir = Path.dirname(fileURLToPath(import.meta.url));
const repoRoot = Path.resolve(smokeDir, "../../../..");

test("clientPersistence smoke: saved environment secret round-trips through storage", () => {
  const dir = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-client-persistence-"));
  const registryPath = Path.join(dir, "saved-environments.json");
  const expectedSecret = "fixture-secret-value";
  const secretStorage: DesktopSecretStorage = {
    isEncryptionAvailable: () => true,
    encryptString: (value) => Buffer.from(`sealed:${value}`, "utf8"),
    decryptString: (value) => value.toString("utf8").replace(/^sealed:/, ""),
  };

  writeSavedEnvironmentRegistry(registryPath, [
    {
      environmentId: "local-demo",
      label: "Local demo",
      httpBaseUrl: "http://127.0.0.1:37123",
      wsBaseUrl: "ws://127.0.0.1:37123/events",
      createdAt: "2026-05-02T00:00:00.000Z",
      lastConnectedAt: null,
    },
  ]);

  expect(
    writeSavedEnvironmentSecret({
      registryPath,
      environmentId: "local-demo",
      secret: expectedSecret,
      secretStorage,
    }),
  ).toBe(true);
  expect(
    readSavedEnvironmentSecret({
      registryPath,
      environmentId: "local-demo",
      secretStorage,
    }),
  ).toBe(expectedSecret);
});

test("serverListeningDetector smoke: readiness is detected across stdout chunks", async () => {
  const detector = new ServerListeningDetector();

  detector.push("booting daemon\nListening");
  detector.push(" on http://127.0.0.1:37123\n");

  await expect(detector.promise).resolves.toBeUndefined();
});

test("runtimeArch smoke: macOS Rosetta and native paths stay distinguishable", () => {
  const rosetta = resolveDesktopRuntimeInfo({
    platform: "darwin",
    processArch: "x64",
    runningUnderArm64Translation: true,
  });
  const nativeArm = resolveDesktopRuntimeInfo({
    platform: "darwin",
    processArch: "arm64",
    runningUnderArm64Translation: false,
  });

  expect(isArm64HostRunningIntelBuild(rosetta)).toBe(true);
  expect(isArm64HostRunningIntelBuild(nativeArm)).toBe(false);
});

test("command registry and keybindings smoke: palette/stage commands dispatch by default", async () => {
  const dir = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-keybindings-"));
  const registry = new CommandRegistry();
  const fired: string[] = [];

  for (const binding of uniqueDefaultCommands()) {
    registry.registerCommand({
      id: binding,
      handler: () => {
        fired.push(binding);
      },
    });
  }

  const manager = new KeybindingsManager({
    configPath: Path.join(dir, "keybindings.json"),
    registry,
    platform: "darwin",
  });

  await manager.dispatch(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    {},
  );
  await manager.dispatch(
    { key: "3", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    {},
  );

  expect(fired).toEqual(["command-palette.open", "stage.swarm"]);
  expect(registry.listCommands().map((command) => command.id)).toContain("activity.toggle");
});

test("build pipeline smoke: DMG and mock-update options are exposed", async () => {
  const result = await runBun(["scripts/build-desktop-artifact.ts", "--help"]);

  expect(result.exitCode).toBe(0);
  expect(result.stdout).toContain("--target <target>");
  expect(result.stdout).toContain("--mock-updates");
  expect(result.stdout).toContain("dmg");
});

test("mock update server smoke: serves update metadata and blocks traversal", async () => {
  const releaseRoot = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-update-server-"));
  const port = 32_000 + Math.floor(Math.random() * 1_000);
  FS.writeFileSync(Path.join(releaseRoot, "latest-mac.yml"), "version: 0.0.0\n", "utf8");
  FS.writeFileSync(Path.join(releaseRoot, "Hoopoe-0.0.0-arm64.dmg"), "fake dmg", "utf8");

  const server = spawn("bun", ["scripts/mock-update-server.ts"], {
    cwd: repoRoot,
    env: {
      ...process.env,
      HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_PORT: String(port),
      HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_ROOT: releaseRoot,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  try {
    await waitForOutput(server, "Hoopoe mock update server listening");

    const metadata = await fetchWithTimeout(`http://localhost:${port}/latest-mac.yml`);
    expect(metadata.status).toBe(200);
    expect(await metadata.text()).toContain("version: 0.0.0");

    const traversal = await fetchWithTimeout(`http://localhost:${port}/..%2Fpackage.json`);
    expect(traversal.status).toBe(404);
  } finally {
    server.kill();
  }
});

async function runBun(args: readonly string[]) {
  const child = spawn("bun", [...args], {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
  });
  const [stdout, stderr, exitCode] = await Promise.all([
    collectStream(child.stdout),
    collectStream(child.stderr),
    new Promise<number>((resolve) => {
      child.on("close", (code) => resolve(code ?? 1));
    }),
  ]);

  return { stdout, stderr, exitCode };
}

async function fetchWithTimeout(url: string): Promise<Response> {
  return globalThis.fetch(url, { signal: AbortSignal.timeout(2_000) });
}

function waitForOutput(
  child: ReturnType<typeof spawn>,
  fragment: string,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      reject(new Error(`Timed out waiting for output: ${fragment}`));
    }, 5_000);

    child.stdout?.on("data", (chunk: Buffer) => {
      if (chunk.toString("utf8").includes(fragment)) {
        clearTimeout(timeout);
        resolve();
      }
    });
    child.on("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    child.on("exit", (code) => {
      if (code !== null && code !== 0) {
        clearTimeout(timeout);
        reject(new Error(`Process exited before readiness: ${code}`));
      }
    });
  });
}

function collectStream(stream: NodeJS.ReadableStream | null): Promise<string> {
  if (!stream) return Promise.resolve("");

  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    stream.on("data", (chunk: Buffer) => chunks.push(chunk));
    stream.on("error", reject);
    stream.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

function uniqueDefaultCommands(): readonly string[] {
  return Array.from(new Set(DEFAULT_KEYBINDINGS.map((binding) => binding.command)));
}
