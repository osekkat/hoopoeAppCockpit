#!/usr/bin/env bun
// Originally from github.com/pingdotgg/t3code (MIT), commit 460d9c3.
// Adapted for Hoopoe's macOS-first desktop release pipeline.

import { constants as fsConstants } from "node:fs";
import { access, cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

type BuildPlatform = "linux" | "mac" | "win";
type BuildArch = "arm64" | "universal" | "x64";

type BuildOptions = {
  platform: BuildPlatform;
  target: string;
  arch: BuildArch;
  version?: string;
  outputDir: string;
  skipBuild: boolean;
  keepStage: boolean;
  signed: boolean;
  verbose: boolean;
  mockUpdates: boolean;
  mockUpdateServerPort?: number;
};

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..");
const rootPackagePath = path.join(repoRoot, "package.json");
const desktopPackagePath = path.join(repoRoot, "apps/desktop/package.json");
const desktopDir = path.join(repoRoot, "apps/desktop");

const platformConfig: Record<BuildPlatform, { flag: string; defaultTarget: string; arch: BuildArch[] }> =
  {
    mac: { flag: "--mac", defaultTarget: "dmg", arch: ["arm64", "x64", "universal"] },
    linux: { flag: "--linux", defaultTarget: "AppImage", arch: ["x64", "arm64"] },
    win: { flag: "--win", defaultTarget: "nsis", arch: ["x64", "arm64"] },
  };

function usage() {
  return [
    "Usage: bun scripts/build-desktop-artifact.ts [options]",
    "",
    "Options:",
    "  --platform <mac|linux|win>          Target platform. Defaults to host platform.",
    "  --target <target>                   electron-builder target. Defaults to dmg/AppImage/nsis.",
    "  --arch <arm64|x64|universal>        Target arch. Defaults to host arch.",
    "  --build-version <version>           Version written into the staged package.",
    "  --output-dir <dir>                  Release output dir. Defaults to release or release-mock.",
    "  --skip-build                        Skip bun run --cwd apps/desktop build.",
    "  --keep-stage                        Keep the temporary staged app directory.",
    "  --signed                            Enable signing/notarization config.",
    "  --verbose                           Stream child command stdout.",
    "  --mock-updates                      Use a generic mock update server publisher.",
    "  --mock-update-server-port <port>     Mock update server port. Defaults to 3000.",
  ].join("\n");
}

function parseBooleanEnv(name: string) {
  const value = process.env[name]?.trim().toLowerCase();
  return value === "1" || value === "true" || value === "yes";
}

function parseArgs(args: string[]) {
  const parsed: Record<string, string | true> = {};

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index] ?? "";
    if (arg.length === 0) continue;
    if (arg === "--help" || arg === "-h") {
      console.log(usage());
      process.exit(0);
    }
    if (!arg.startsWith("--")) {
      throw new Error(`Unexpected positional argument: ${arg}`);
    }

    const [rawKey, inlineValue] = arg.slice(2).split("=", 2);
    if (!rawKey) throw new Error(`Invalid option: ${arg}`);
    if (inlineValue !== undefined) {
      parsed[rawKey] = inlineValue;
      continue;
    }

    const next = args[index + 1];
    if (!next || next.startsWith("--")) {
      parsed[rawKey] = true;
      continue;
    }

    parsed[rawKey] = next;
    index += 1;
  }

  return parsed;
}

function readStringOption(parsed: Record<string, string | true>, name: string) {
  const value = parsed[name];
  if (value === undefined || value === true) return undefined;
  return value;
}

function readBooleanOption(parsed: Record<string, string | true>, name: string) {
  const value = parsed[name];
  if (value === undefined) return undefined;
  if (value === true) return true;
  return ["1", "true", "yes"].includes(value.trim().toLowerCase());
}

function detectHostPlatform(): BuildPlatform {
  if (process.platform === "darwin") return "mac";
  if (process.platform === "linux") return "linux";
  if (process.platform === "win32") return "win";
  throw new Error(`Unsupported host platform: ${process.platform}`);
}

function detectHostArch(): BuildArch {
  if (process.arch === "arm64") return "arm64";
  return "x64";
}

function parsePlatform(value: string | undefined): BuildPlatform | undefined {
  if (value === undefined) return undefined;
  if (value === "mac" || value === "linux" || value === "win") return value;
  throw new Error(`Unsupported platform: ${value}`);
}

function parseArch(value: string | undefined): BuildArch | undefined {
  if (value === undefined) return undefined;
  if (value === "arm64" || value === "x64" || value === "universal") return value;
  throw new Error(`Unsupported arch: ${value}`);
}

function parsePort(value: string | undefined): number | undefined {
  if (value === undefined || value.trim().length === 0) return undefined;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid mock update server port: ${value}`);
  }
  return port;
}

function resolveOptions(): BuildOptions {
  const parsed = parseArgs(process.argv.slice(2));
  const platform =
    parsePlatform(readStringOption(parsed, "platform") ?? process.env.HOOPOE_DESKTOP_PLATFORM) ??
    detectHostPlatform();
  const target =
    readStringOption(parsed, "target") ??
    process.env.HOOPOE_DESKTOP_TARGET ??
    platformConfig[platform].defaultTarget;
  const arch =
    parseArch(readStringOption(parsed, "arch") ?? process.env.HOOPOE_DESKTOP_ARCH) ??
    detectHostArch();

  if (!platformConfig[platform].arch.includes(arch)) {
    throw new Error(`Arch ${arch} is not supported for platform ${platform}`);
  }

  const mockUpdates =
    readBooleanOption(parsed, "mock-updates") ?? parseBooleanEnv("HOOPOE_DESKTOP_MOCK_UPDATES");
  const defaultOutputDir = mockUpdates ? "release-mock" : "release";
  const outputDir = path.resolve(
    repoRoot,
    readStringOption(parsed, "output-dir") ??
      process.env.HOOPOE_DESKTOP_OUTPUT_DIR ??
      defaultOutputDir,
  );

  return {
    platform,
    target,
    arch,
    version:
      readStringOption(parsed, "build-version") ?? process.env.HOOPOE_DESKTOP_VERSION ?? undefined,
    outputDir,
    skipBuild:
      readBooleanOption(parsed, "skip-build") ?? parseBooleanEnv("HOOPOE_DESKTOP_SKIP_BUILD"),
    keepStage:
      readBooleanOption(parsed, "keep-stage") ?? parseBooleanEnv("HOOPOE_DESKTOP_KEEP_STAGE"),
    signed: readBooleanOption(parsed, "signed") ?? parseBooleanEnv("HOOPOE_DESKTOP_SIGNED"),
    verbose: readBooleanOption(parsed, "verbose") ?? parseBooleanEnv("HOOPOE_DESKTOP_VERBOSE"),
    mockUpdates,
    mockUpdateServerPort: parsePort(
      readStringOption(parsed, "mock-update-server-port") ??
        process.env.HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_PORT,
    ),
  };
}

async function readJsonFile<T>(filePath: string): Promise<T> {
  try {
    return JSON.parse(await readFile(filePath, "utf8")) as T;
  } catch (cause) {
    throw new Error(`Unable to parse JSON file ${filePath}`, { cause });
  }
}

async function exists(filePath: string) {
  try {
    await access(filePath, fsConstants.F_OK);
    return true;
  } catch {
    return false;
  }
}

async function run(command: string, args: string[], cwd: string, verbose: boolean) {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env: process.env,
      shell: process.platform === "win32",
      stdio: verbose ? "inherit" : ["ignore", "ignore", "inherit"],
    });

    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} exited with code ${code ?? "unknown"}`));
    });
  });
}

async function resolveGitCommitHash() {
  const output = await new Promise<string>((resolve) => {
    const child = spawn("git", ["rev-parse", "--short=12", "HEAD"], {
      cwd: repoRoot,
      stdio: ["ignore", "pipe", "ignore"],
    });
    let stdout = "";
    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
    });
    child.on("error", () => resolve("unknown"));
    child.on("exit", (code) => resolve(code === 0 ? stdout : "unknown"));
  });
  const hash = output.trim().toLowerCase();
  return /^[0-9a-f]{7,40}$/.test(hash) ? hash : "unknown";
}

function resolveCatalogDependencies(
  dependencies: Record<string, string> | undefined,
  catalog: Record<string, string>,
) {
  if (!dependencies) return {};

  return Object.fromEntries(
    Object.entries(dependencies)
      .filter(([name]) => name !== "electron")
      .map(([name, spec]) => {
        if (!spec.startsWith("catalog:")) return [name, spec];
        const key = spec.slice("catalog:".length).trim() || name;
        const resolved = catalog[key];
        if (!resolved) {
          throw new Error(`Unable to resolve catalog dependency ${name} from key ${key}`);
        }
        return [name, resolved];
      }),
  );
}

function resolveUpdateChannel(version: string) {
  return /-nightly\.\d{8}\.\d+$/.test(version) ? "nightly" : "latest";
}

function resolvePublishConfig(version: string, mockUpdates: boolean, mockUpdateServerPort?: number) {
  if (mockUpdates) {
    return [
      {
        provider: "generic",
        url: `http://localhost:${mockUpdateServerPort ?? 3000}`,
      },
    ];
  }

  const rawRepo = process.env.HOOPOE_DESKTOP_UPDATE_REPOSITORY ?? process.env.GITHUB_REPOSITORY;
  const [owner, repo, extra] = rawRepo?.split("/") ?? [];
  if (!owner || !repo || extra) return undefined;

  const channel = resolveUpdateChannel(version);
  return [
    {
      provider: "github",
      owner,
      repo,
      releaseType: channel === "nightly" ? "prerelease" : "release",
      ...(channel === "nightly" ? { channel } : {}),
    },
  ];
}

function createBuildConfig(options: BuildOptions, productName: string) {
  const buildConfig: Record<string, unknown> = {
    appId: "com.hoopoe.app",
    productName,
    artifactName: "Hoopoe-${version}-${arch}.${ext}",
    directories: {
      buildResources: "apps/desktop/resources",
    },
  };

  const publish = resolvePublishConfig(
    options.version ?? "0.0.0",
    options.mockUpdates,
    options.mockUpdateServerPort,
  );
  if (publish) buildConfig.publish = publish;

  if (options.platform === "mac") {
    buildConfig.mac = {
      target: options.target === "dmg" ? ["dmg", "zip"] : [options.target],
      category: "public.app-category.developer-tools",
      hardenedRuntime: true,
      gatekeeperAssess: false,
      ...(options.signed
        ? {
            notarize: {
              appleApiKey: process.env.APPLE_API_KEY,
              appleApiKeyId: process.env.APPLE_API_KEY_ID,
              appleApiIssuer: process.env.APPLE_API_ISSUER,
            },
          }
        : {}),
    };
  }

  if (options.platform === "linux") {
    buildConfig.linux = {
      target: [options.target],
      executableName: "hoopoe",
      category: "Development",
    };
  }

  if (options.platform === "win") {
    buildConfig.win = {
      target: [options.target],
    };
  }

  return buildConfig;
}

async function stageDirectory(source: string, target: string) {
  if (!(await exists(source))) return;
  await mkdir(target, { recursive: true });
  await cp(source, target, { recursive: true, force: true });
}

async function stageApp(options: BuildOptions) {
  const rootPackage = await readJsonFile<{
    workspaces?: { catalog?: Record<string, string> };
  }>(rootPackagePath);
  const desktopPackage = await readJsonFile<{
    dependencies?: Record<string, string>;
    devDependencies?: Record<string, string>;
    productName?: string;
    version?: string;
  }>(desktopPackagePath);

  const version = options.version ?? desktopPackage.version ?? "0.0.0";
  const stageDir = await mkdtemp(path.join(os.tmpdir(), "hoopoe-desktop-stage-"));
  const stageResourcesDir = path.join(stageDir, "resources");

  await mkdir(stageResourcesDir, { recursive: true });
  await mkdir(options.outputDir, { recursive: true });

  if (!options.skipBuild) {
    await run("bun", ["run", "--cwd", "apps/desktop", "build"], repoRoot, options.verbose);
  }

  await stageDirectory(path.join(desktopDir, "dist"), path.join(stageDir, "dist"));
  await stageDirectory(path.join(desktopDir, "dist-electron"), path.join(stageDir, "dist-electron"));

  const mainCandidates = [
    path.join(stageDir, "dist-electron/main.js"),
    path.join(stageDir, "dist/main.js"),
    path.join(stageDir, "main.js"),
  ];
  let main: string | undefined;
  for (const candidate of mainCandidates) {
    if (await exists(candidate)) {
      main = candidate;
      break;
    }
  }
  if (!main) {
    throw new Error(
      "Desktop build output is missing a main process bundle. Expected dist-electron/main.js.",
    );
  }

  const catalog = rootPackage.workspaces?.catalog ?? {};
  const dependencies = resolveCatalogDependencies(desktopPackage.dependencies, catalog);
  const electronVersion =
    desktopPackage.devDependencies?.electron ?? catalog.electron ?? process.env.ELECTRON_VERSION;
  if (!electronVersion) {
    throw new Error("electron version is missing from apps/desktop package.json or catalog");
  }

  const packageJson = {
    name: "@hoopoe/desktop-artifact",
    version,
    buildVersion: version,
    hoopoeCommitHash: await resolveGitCommitHash(),
    private: true,
    description: "Hoopoe desktop release artifact staging package",
    author: "Hoopoe",
    main: path.relative(stageDir, main),
    build: createBuildConfig(options, desktopPackage.productName ?? "Hoopoe"),
    dependencies,
    devDependencies: { electron: electronVersion },
  };

  await writeFile(path.join(stageDir, "package.json"), `${JSON.stringify(packageJson, null, 2)}\n`);

  return { stageDir };
}

async function main() {
  const options = resolveOptions();
  const { stageDir } = await stageApp(options);

  try {
    await run("bun", ["install", "--production"], stageDir, options.verbose);
    await run(
      "bunx",
      [
        "electron-builder",
        platformConfig[options.platform].flag,
        options.target,
        `--${options.arch}`,
        "--publish",
        "never",
        "--config",
        "package.json",
      ],
      stageDir,
      options.verbose,
    );

    if ((await exists(path.join(stageDir, "release")))) {
      await stageDirectory(path.join(stageDir, "release"), options.outputDir);
    }
  } finally {
    if (options.keepStage) {
      console.log(`Keeping staged desktop app at ${stageDir}`);
    } else if (stageDir.startsWith(os.tmpdir())) {
      await rm(stageDir, { recursive: true, force: true });
    }
  }
}

if (import.meta.main) {
  main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  });
}
