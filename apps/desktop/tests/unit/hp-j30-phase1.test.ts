import { spawn } from "node:child_process";
import * as FS from "node:fs";
import * as OS from "node:os";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "bun:test";
import { CommandRegistry, UnknownContextKeyError } from "../../src/main/CommandRegistry.ts";
import { spawnBackend } from "../../src/main/BackendLifecycle.ts";
import { SettingsBridge, type SettingsChangeEvent } from "../../src/main/SettingsBridge.ts";
import { KeybindingsManager } from "../../src/main/keybindings/index.ts";
import {
  createUpdateMachine,
  shouldBroadcastDownloadProgress,
} from "../../src/main/UpdateMachine.ts";
import {
  EXPECTED_PHASE1_MOCK_TOKENS,
  assertNoProductionEndpoints,
  createStructuredLogger,
  createPhase1MockFlywheelHarness,
  createPhase1TestLogger,
} from "../../src/test-utils/index.ts";
import {
  UnknownCommandPaletteContextKeyError,
  getCommandPaletteModel,
  moveCommandPaletteSelection,
  type CommandPaletteCategory,
  type CommandPaletteCommand,
} from "@hoopoe/design-system";

const unitDir = Path.dirname(fileURLToPath(import.meta.url));
const repoRoot = Path.resolve(unitDir, "../../../..");

test("hp-j30 lifecycle: real daemon subprocess reports ready and stops cleanly", async () => {
  const logger = createPhase1TestLogger({
    suite: "hp-j30.unit",
    testName: "lifecycle",
  });
  logger.start();
  logger.phase("setup");
  const binary = writeMockDaemonBinary();
  let handle: Awaited<ReturnType<typeof spawnBackend>> | null = null;

  try {
    logger.phase("act");
    handle = await spawnBackend({
      daemonBinaryPath: binary,
      logger: {
        info: (message, meta) => {
          logger.snapshot(message, meta ?? {});
        },
        warn: (message, meta) => {
          logger.snapshot(message, meta ?? {});
        },
        error: (message, meta) => {
          logger.snapshot(message, meta ?? {});
        },
      },
      readinessOptions: {
        path: "/health",
        intervalMs: 10,
        requestTimeoutMs: 250,
        timeoutMs: 3_000,
      },
    });

    logger.phase("assert");
    assertNoProductionEndpoints({ urls: [handle.baseUrl] });
    const health = await fetch(`${handle.baseUrl}/health`, {
      signal: AbortSignal.timeout(2_000),
    });
    logger.snapshot("backend.health", {
      status: health.status,
      baseUrl: handle.baseUrl,
    });
    expect(health.status).toBe(200);
    expect(await health.json()).toEqual({ status: "ok", environment: "hp-j30" });
  } finally {
    logger.phase("teardown");
    await handle?.stop({ graceMs: 500 });
  }

  logger.end("passed");
  expect(
    logger
      .entries()
      .map((entry) => entry.component)
      .every((value) => value === "test.phase1"),
  ).toBe(true);
});

test("hp-j30 settings: user/project overlays hot-apply and relaunch keys log reasons", async () => {
  const logger = createPhase1TestLogger({
    suite: "hp-j30.unit",
    testName: "settings",
  });
  const root = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-hp-j30-settings-"));
  const userFile = Path.join(root, "user", "settings.json");
  const projectFile = Path.join(root, "project", ".hoopoe", "settings.json");
  const changes: SettingsChangeEvent[] = [];
  const relaunchReasons: string[] = [];
  const structured = createStructuredLogger({
    suite: "hp-j30.real-service",
    testId: "settings-bridge-atomic-write",
  });
  logger.start({
    userFile: "tmp:user/settings.json",
    projectFile: "tmp:project/.hoopoe/settings.json",
  });

  const bridge = new SettingsBridge({
    paths: { userFile, projectFile },
    relaunch: (reason) => {
      relaunchReasons.push(reason);
      logger.snapshot("settings.relaunch", { reason });
    },
  });
  bridge.subscribe((event) => {
    changes.push(event);
    logger.snapshot("settings.change", event);
  });

  logger.phase("act");
  await structured.step("settings-bridge.atomic-write", async () => {
    bridge.setUserSettings({
      client: { activeStage: "beads", activityPanelOpen: true },
      desktop: { updateChannel: "nightly", updateChannelConfiguredByUser: true },
    });
    bridge.setProjectSettings({
      client: { activeStage: "swarm" },
      daemon: { daemonBinaryPath: "/opt/hoopoe/bin/daemon" },
    });
    await waitForMicrotask();
  }, {
    userFile: "tmp:user/settings.json",
    projectFile: "tmp:project/.hoopoe/settings.json",
  });

  logger.phase("assert");
  expect(bridge.resolved().client.activeStage).toBe("swarm");
  expect(bridge.resolved().client.activityPanelOpen).toBe(true);
  expect(bridge.resolved().desktop.updateChannel).toBe("nightly");
  expect(relaunchReasons).toHaveLength(1);
  expect(relaunchReasons[0]).toContain("daemon.daemonBinaryPath");
  expect(changes.flatMap((event) => event.changedKeys)).toContain("client.activeStage");
  expect(readSettingsJson(projectFile).schemaVersion).toBe(1);
  structured.stepSync("settings-bridge.fs-assert", () => {
    expect(FS.existsSync(userFile)).toBe(true);
    expect(FS.existsSync(projectFile)).toBe(true);
    expect(
      FS.readdirSync(Path.dirname(userFile)).filter((name) => name.endsWith(".tmp")),
    ).toEqual([]);
    expect(
      FS.readdirSync(Path.dirname(projectFile)).filter((name) => name.endsWith(".tmp")),
    ).toEqual([]);
  });
  logger.snapshot("settings.structured-real-service-log", {
    entries: structured.entries().length,
    lastStatus: structured.entries().at(-1)?.status ?? null,
  });
  logger.end("passed", { changeEvents: changes.length });
});

test("hp-j30 keybindings + command palette: cmd+k, last-rule wins, fuzzy search, when filters", async () => {
  const logger = createPhase1TestLogger({
    suite: "hp-j30.unit",
    testName: "keybindings-command-palette",
  });
  const root = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-hp-j30-keys-"));
  const configPath = Path.join(root, "keybindings.json");
  const registry = new CommandRegistry();
  const fired: string[] = [];
  logger.start();

  registerPaletteCommand(registry, "command-palette.open", "Open Command Palette", "Window", fired);
  registerPaletteCommand(registry, "stage.swarm", "Go to Swarm", "Swarm", fired);
  registerPaletteCommand(registry, "approval.review", "Review Approval", "Activity", fired);
  FS.writeFileSync(
    configPath,
    `${JSON.stringify(
      [
        { key: "cmd+k", command: "command-palette.open", when: "!commandPalette.open" },
        { key: "cmd+k", command: "stage.swarm", when: "stage.swarm && !commandPalette.open" },
      ],
      null,
      2,
    )}\n`,
    "utf8",
  );

  logger.phase("act");
  const manager = new KeybindingsManager({
    configPath,
    registry,
    platform: "darwin",
    debounceMs: 5,
  });
  await manager.dispatch(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    { "stage.swarm": false, "commandPalette.open": false },
  );
  await manager.dispatch(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    { "stage.swarm": true, "commandPalette.open": false },
  );

  const paletteModel = getCommandPaletteModel({
    commands: paletteCommands(registry),
    query: "palette",
    context: { "approvals.pending": false, "project.active": true },
    knownContextKeys: registry.knownContextKeys(),
    recentCommandIds: ["stage.swarm"],
  });
  const allCommandsModel = getCommandPaletteModel({
    commands: paletteCommands(registry),
    query: "",
    context: { "approvals.pending": false, "project.active": true },
    knownContextKeys: registry.knownContextKeys(),
    recentCommandIds: ["stage.swarm"],
    activeCommandId: "command-palette.open",
  });
  const moved = moveCommandPaletteSelection(allCommandsModel, "next");
  logger.snapshot("palette.model", {
    visible: paletteModel.items.map((item) => item.command.id),
    filtered: paletteModel.filteredCommandIds,
    moved,
  });

  logger.phase("assert");
  expect(fired).toEqual(["command-palette.open", "stage.swarm"]);
  expect(paletteModel.items[0]?.command.id).toBe("command-palette.open");
  expect(paletteModel.filteredCommandIds).toContain("approval.review");
  expect(moved).toBe("stage.swarm");

  FS.writeFileSync(
    configPath,
    `${JSON.stringify([{ key: "cmd+u", command: "stage.swarm", when: "stage.swarn" }])}\n`,
    "utf8",
  );
  expect(() => manager.reloadNow()).toThrow(UnknownContextKeyError);
  expect(() =>
    getCommandPaletteModel({
      commands: [
        {
          id: "unknown.when",
          title: "Unknown When",
          category: "Help",
          whenContextKeys: ["missing.context"],
        },
      ],
      query: "",
      context: {},
      knownContextKeys: [],
    }),
  ).toThrow(UnknownCommandPaletteContextKeyError);
  logger.end("passed");
});

test("hp-j30 Mock Flywheel harness: fixture corpus boots daemon-shaped RPC and replay paths", async () => {
  const logger = createPhase1TestLogger({
    suite: "hp-j30.unit",
    testName: "mock-flywheel",
  });
  logger.start();
  logger.phase("setup");
  const harness = createPhase1MockFlywheelHarness({ scenarioId: "healthy-hour" });

  try {
    logger.phase("act");
    const ready = await harness.assertReady();
    const auth = await harness.authRoundTrip();
    const replay = await harness.collectReplayEvents();
    logger.snapshot("mock-flywheel.ready", ready);
    logger.snapshot("mock-flywheel.replay", {
      eventCount: replay.events.length,
      channels: Object.keys(replay.cursors).toSorted(),
    });

    logger.phase("assert");
    expect(harness.availableScenarioIds()).toContain("healthy-hour");
    expect(auth).toEqual(EXPECTED_PHASE1_MOCK_TOKENS);
    expect(ready.healthEnvironment).toBe("mock-flywheel");
    expect(ready.projectId).toBe("mock-flywheel-project");
    expect(replay.events.length).toBeGreaterThan(0);
    expect(Object.keys(replay.cursors).length).toBeGreaterThan(0);
    expect(harness.ipcRegistry.size()).toBeGreaterThan(0);
    logger.end("passed");
  } finally {
    harness.close();
    expect(harness.ipcRegistry.size()).toBe(0);
  }
});

test("hp-j30 update + DMG smoke: channel state transitions and build help stay wired", async () => {
  const logger = createPhase1TestLogger({
    suite: "hp-j30.unit",
    testName: "update-dmg-smoke",
  });
  logger.start();
  logger.phase("act");
  const machine = createUpdateMachine({
    currentVersion: "0.0.0",
    channel: "nightly",
    runtimeInfo: {
      hostArch: "arm64",
      appArch: "arm64",
      runningUnderArm64Translation: false,
    },
  });
  const checked = machine.onCheckStart("2026-05-03T00:00:00.000Z");
  const available = machine.onUpdateAvailable("0.1.0", "2026-05-03T00:00:01.000Z");
  machine.onDownloadStart();
  const downloading = machine.onDownloadProgress(57);
  const downloaded = machine.onDownloadComplete("0.1.0");
  const help = await runBun(["scripts/build-desktop-artifact.ts", "--help"]);
  logger.snapshot("update.transitions", {
    checked: checked.status,
    available: available.status,
    downloading: downloading.downloadPercent,
    downloaded: downloaded.status,
    broadcastNextStep: shouldBroadcastDownloadProgress(downloading, 61),
  });
  logger.snapshot("build-desktop-artifact.help", {
    exitCode: help.exitCode,
    stdoutLength: help.stdout.length,
    stderrLength: help.stderr.length,
  });

  logger.phase("assert");
  expect(machine.current().channel).toBe("nightly");
  expect(downloaded.status).toBe("downloaded");
  expect(downloaded.downloadedVersion).toBe("0.1.0");
  expect(shouldBroadcastDownloadProgress(downloading, 61)).toBe(true);
  expect(help.exitCode).toBe(0);
  expect(help.stdout).toContain("--target <target>");
  expect(help.stdout).toContain("--mock-updates");
  expect(help.stdout).toContain("dmg");
  logger.end("passed");
});

function writeMockDaemonBinary(): string {
  const root = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-hp-j30-daemon-"));
  const daemonPath = Path.join(root, "daemon.mjs");
  const binaryPath = Path.join(root, "hoopoe-daemon");
  FS.writeFileSync(
    daemonPath,
    `import http from "node:http";
const args = process.argv.slice(2);
const getArg = (name, fallback) => {
  const index = args.indexOf(name);
  return index === -1 ? fallback : args[index + 1] ?? fallback;
};
const host = getArg("--host", "127.0.0.1");
const port = Number(getArg("--port", "0"));
const server = http.createServer((incoming, response) => {
  if (incoming.url === "/health") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ status: "ok", environment: "hp-j30" }));
    return;
  }
  response.writeHead(404);
  response.end("not found");
});
server.listen(port, host, () => {
  process.stdout.write(\`Listening on http://\${host}:\${port}\\n\`);
});
process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
`,
    "utf8",
  );
  FS.writeFileSync(
    binaryPath,
    `#!/usr/bin/env sh\nexec "${process.execPath}" "${daemonPath}" "$@"\n`,
    "utf8",
  );
  FS.chmodSync(binaryPath, 0o755);
  return binaryPath;
}

function registerPaletteCommand(
  registry: CommandRegistry,
  id: string,
  title: string,
  category: CommandPaletteCategory,
  fired: string[],
): void {
  registry.registerCommand({
    id,
    title,
    category,
    handler: () => {
      fired.push(id);
    },
  });
}

function paletteCommands(registry: CommandRegistry): readonly CommandPaletteCommand[] {
  return registry.listCommands().map((command): CommandPaletteCommand => {
    const category = isPaletteCategory(command.category) ? command.category : "Help";
    return {
      id: command.id,
      title: command.title ?? command.id,
      category,
      ...(command.id === "approval.review" ? { whenContextKeys: ["approvals.pending"] } : {}),
    };
  });
}

function isPaletteCategory(value: unknown): value is CommandPaletteCategory {
  return (
    value === "Project" ||
    value === "Plan" ||
    value === "Beads" ||
    value === "Swarm" ||
    value === "Activity" ||
    value === "Diagnostics" ||
    value === "Help" ||
    value === "Window"
  );
}

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

function collectStream(stream: NodeJS.ReadableStream | null): Promise<string> {
  if (!stream) return Promise.resolve("");

  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    stream.on("data", (chunk: Buffer) => chunks.push(chunk));
    stream.on("error", reject);
    stream.on("end", () => {
      resolve(Buffer.concat(chunks).toString("utf8"));
    });
  });
}

function readSettingsJson(filePath: string): Record<string, unknown> {
  const raw = FS.readFileSync(filePath, "utf8");
  try {
    const parsed = JSON.parse(raw);
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("settings file did not contain a JSON object");
    }
    return parsed as Record<string, unknown>;
  } catch (error) {
    throw new Error(
      `Failed to parse settings JSON at ${filePath}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
  }
}

async function waitForMicrotask(): Promise<void> {
  await new Promise<void>((resolve) => {
    setImmediate(resolve);
  });
}
