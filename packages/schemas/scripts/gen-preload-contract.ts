#!/usr/bin/env bun
//
// gen-preload-contract.ts — regenerate the renderer ↔ preload ↔ main IPC
// contract TS module from `packages/schemas/preload-api.yaml`.
//
// Default output: `apps/desktop/src/shared/ipc-contract.gen.ts`
//
// The existing manual `apps/desktop/src/shared/ipc-contract.ts` is the
// hand-rolled hp-n5za hardening pass. When the apps/desktop owner switches
// it to import-and-re-export from `ipc-contract.gen.ts`, the manual file
// becomes a thin shim and the generated file is the source. Until then,
// the parity test (`apps/desktop/src/shared/ipc-contract.test.ts`)
// compares the two and CI fails on drift.
//
// Run locally: `bun run --cwd packages/schemas generate:preload`
// CI gate:     `bun run --cwd packages/schemas validate:preload`

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const repoRoot = resolve(pkgRoot, "..", "..");
const yamlPath = resolve(pkgRoot, "preload-api.yaml");
const defaultOutPath = resolve(repoRoot, "apps/desktop/src/shared/ipc-contract.gen.ts");

type GeneratorOptions = {
  outPath: string | null;
  stdout: boolean;
};

interface PreloadApi {
  schemaVersion: number;
  daemonRequestMethods: Record<string, { description: string; bead: string }>;
  daemonSubscribeTopics: Record<string, { description: string; bead: string }>;
  preloadChannels: Record<string, string>;
  preloadChannelContracts: Record<
    string,
    { channel: string; input: string; output: string }
  >;
  mockFlywheelCommands: Record<string, string>;
  internalCommands: Record<string, string>;
}

function parseYaml(text: string): PreloadApi {
  // Minimal hand-rolled YAML loader — enough for our flat shape.
  // We don't pull in `yaml` because then we'd need to add it as a dep,
  // and bunx wrappers don't compose cleanly here.
  const lines = text.split("\n");
  const result: PreloadApi = {
    schemaVersion: 0,
    daemonRequestMethods: {},
    daemonSubscribeTopics: {},
    preloadChannels: {},
    preloadChannelContracts: {},
    mockFlywheelCommands: {},
    internalCommands: {},
  };

  type Section =
    | "none"
    | "daemonRequestMethods"
    | "daemonSubscribeTopics"
    | "preloadChannels"
    | "preloadChannelContracts"
    | "mockFlywheelCommands"
    | "internalCommands";
  let section: Section = "none";
  let currentKey: string | null = null;
  let currentPreloadContractKey: string | null = null;
  let currentBead = "";
  let currentDescription = "";

  // Flush the in-flight entry into the right section when leaving it.
  function flushPending(): void {
    if (
      currentKey !== null &&
      (section === "daemonRequestMethods" || section === "daemonSubscribeTopics")
    ) {
      (result as any)[section][currentKey] = {
        description: currentDescription.trim(),
        bead: currentBead,
      };
    }
    currentKey = null;
    currentDescription = "";
    currentBead = "";
  }

  for (const rawLine of lines) {
    const line = rawLine.replace(/\r$/, "");
    if (line.trim().startsWith("#") || line.trim() === "") continue;

    // Top-level: schemaVersion, mirrorsTsFile, section headers
    if (/^schemaVersion:\s*(\d+)/.test(line)) {
      flushPending();
      result.schemaVersion = Number(line.split(":")[1]?.trim());
      continue;
    }
    if (/^mirrorsTsFile:/.test(line)) {
      flushPending();
      continue;
    }

    if (/^daemonRequestMethods:\s*$/.test(line)) {
      flushPending();
      section = "daemonRequestMethods";
      continue;
    }
    if (/^daemonSubscribeTopics:\s*$/.test(line)) {
      flushPending();
      section = "daemonSubscribeTopics";
      continue;
    }
    if (/^preloadChannels:\s*$/.test(line)) {
      flushPending();
      section = "preloadChannels";
      currentPreloadContractKey = null;
      continue;
    }
    if (/^preloadChannelContracts:\s*$/.test(line)) {
      flushPending();
      section = "preloadChannelContracts";
      currentPreloadContractKey = null;
      continue;
    }
    if (/^mockFlywheelCommands:\s*$/.test(line)) {
      flushPending();
      section = "mockFlywheelCommands";
      continue;
    }
    if (/^internalCommands:\s*$/.test(line)) {
      flushPending();
      section = "internalCommands";
      continue;
    }
    if (/^[a-zA-Z]/.test(line)) {
      // New top-level — leaving the current section.
      flushPending();
      section = "none";
      currentPreloadContractKey = null;
      continue;
    }

    if (section === "mockFlywheelCommands" || section === "internalCommands") {
      const m = /^\s+([a-zA-Z]\w*):\s+(\S+)\s*$/.exec(line);
      if (m && m[1] !== undefined && m[2] !== undefined) {
        result[section][m[1]] = m[2];
      }
      continue;
    }

    if (section === "preloadChannels") {
      const m = /^\s+([a-zA-Z]\w*):\s+(\S+)\s*$/.exec(line);
      if (m && m[1] !== undefined && m[2] !== undefined) {
        result.preloadChannels[m[1]] = m[2];
      }
      continue;
    }

    if (section === "preloadChannelContracts") {
      const keyMatch = /^  ([a-zA-Z]\w*):\s*$/.exec(line);
      if (keyMatch && keyMatch[1] !== undefined) {
        currentPreloadContractKey = keyMatch[1];
        result.preloadChannelContracts[currentPreloadContractKey] = {
          channel: "",
          input: "",
          output: "",
        };
        continue;
      }

      const propMatch = /^\s{4}(channel|input|output):\s+(\S+)\s*$/.exec(line);
      if (
        propMatch &&
        currentPreloadContractKey !== null &&
        propMatch[1] !== undefined &&
        propMatch[2] !== undefined
      ) {
        const contract = result.preloadChannelContracts[currentPreloadContractKey];
        if (contract !== undefined) {
          contract[propMatch[1] as "channel" | "input" | "output"] = propMatch[2];
        }
      }
      continue;
    }

    if (section === "daemonRequestMethods" || section === "daemonSubscribeTopics") {
      // 2-space indent → method/topic name; deeper indent → its body.
      const m2 = /^  ([a-zA-Z][\w.-]*):\s*$/.exec(line);
      if (m2 && m2[1] !== undefined) {
        flushPending();
        currentKey = m2[1];
        continue;
      }
      const beadMatch = /^\s+bead:\s+(\S+)/.exec(line);
      if (beadMatch && beadMatch[1] !== undefined) {
        currentBead = beadMatch[1];
        continue;
      }
      const descMatch = /^\s+description:\s+(.*)/.exec(line);
      if (descMatch && descMatch[1] !== undefined) {
        currentDescription = descMatch[1].trim();
      }
    }
  }

  // Flush trailing entry (covers the last YAML key in the file).
  flushPending();

  return result;
}

// hp-3zc: every direct preload channel must declare typed input/output —
// EXCEPT for the multiplexers and the subscribe-only watch channel, which
// are not invoke-style request/response and carry their payload typing in
// §1 daemonRequestMethods + §2 daemonSubscribeTopics. Adding a new direct
// channel without a corresponding contract entry should fail the build.
const PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT: ReadonlySet<string> = new Set([
  "daemonRequest",
  "daemonSubscribe",
  "daemonUnsubscribe",
  "settingsWatch",
]);

function assertValidPreloadContracts(api: PreloadApi): void {
  for (const [key, contract] of Object.entries(api.preloadChannelContracts)) {
    if (!contract.channel || !contract.input || !contract.output) {
      throw new Error(`preloadChannelContracts.${key} must declare channel, input, and output`);
    }
    if (contract.channel !== key) {
      throw new Error(
        `preloadChannelContracts.${key}.channel must match the contract key, got ${contract.channel}`,
      );
    }
    if (!(contract.channel in api.preloadChannels)) {
      throw new Error(
        `preloadChannelContracts.${key}.channel references unknown preloadChannels key ${contract.channel}`,
      );
    }
  }
  // Coverage gate (hp-3zc): every direct (invoke-style) preload channel must
  // appear in preloadChannelContracts. Forgetting an entry is a documented
  // contract gap that the codegen + drift gate should reject.
  const missingContracts: string[] = [];
  for (const channelKey of Object.keys(api.preloadChannels)) {
    if (PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT.has(channelKey)) continue;
    if (!(channelKey in api.preloadChannelContracts)) {
      missingContracts.push(channelKey);
    }
  }
  if (missingContracts.length > 0) {
    throw new Error(
      `preloadChannelContracts is missing entries for direct preload channels: ${missingContracts.join(", ")}. ` +
        "Add a {channel, input, output} contract entry per channel, or extend " +
        "PRELOAD_CHANNELS_WITHOUT_DIRECT_CONTRACT in this script if the channel " +
        "is not invoke-style.",
    );
  }
}

function generateTs(api: PreloadApi): string {
  const requestMethodNames = Object.keys(api.daemonRequestMethods);
  const subscribeTopicNames = Object.keys(api.daemonSubscribeTopics);
  const channelEntries = Object.entries(api.preloadChannels);
  const preloadContractEntries = Object.entries(api.preloadChannelContracts);
  const mockCommandEntries = Object.entries(api.mockFlywheelCommands);
  const internalCommandEntries = Object.entries(api.internalCommands);

  const fmtList = (xs: readonly string[]): string =>
    xs.map((x) => `  ${JSON.stringify(x)},`).join("\n");

  const fmtObject = (entries: readonly (readonly [string, string])[]): string =>
    entries.map(([key, value]) => `  ${key}: ${JSON.stringify(value)},`).join("\n");

  const channelLines = channelEntries
    .map(([key, value]) => `  ${key}: ${JSON.stringify(value)},`)
    .join("\n");

  const preloadContractLines = preloadContractEntries
    .map(
      ([key, value]) =>
        `  ${key}: {\n` +
        `    channel: PRELOAD_IPC_CHANNELS.${value.channel},\n` +
        `    input: ${JSON.stringify(value.input)},\n` +
        `    output: ${JSON.stringify(value.output)},\n` +
        "  },",
    )
    .join("\n");

  return `/**
 * GENERATED — DO NOT EDIT.
 *
 * Source: packages/schemas/preload-api.yaml (schemaVersion ${api.schemaVersion}).
 * Generator: packages/schemas/scripts/gen-preload-contract.ts.
 * Drift gate: packages/schemas/scripts/validate-preload-codegen.ts (CI).
 *
 * This file mirrors the renderer ↔ preload ↔ main IPC allowlist that lives
 * authoritatively in the YAML. The hand-rolled apps/desktop/src/shared/
 * ipc-contract.ts (hp-n5za hardening) pre-dates this generator; the parity
 * test in that directory enforces that the two cannot drift. When the
 * desktop owner switches the manual file to import from this one, the
 * manual file becomes a thin shim.
 *
 * Threat model: every entry here expands the renderer's reach. Adding one
 * is a security-relevant change. Review the bead in the YAML entry before
 * extending.
 */

export const DAEMON_REQUEST_METHODS = [
${fmtList(requestMethodNames)}
] as const;

export type DaemonRequestMethod = (typeof DAEMON_REQUEST_METHODS)[number];

const DAEMON_REQUEST_METHOD_SET: ReadonlySet<string> = new Set(DAEMON_REQUEST_METHODS);

export function isDaemonRequestMethod(value: unknown): value is DaemonRequestMethod {
  return typeof value === "string" && DAEMON_REQUEST_METHOD_SET.has(value);
}

export const DAEMON_SUBSCRIBE_TOPICS = [
${fmtList(subscribeTopicNames)}
] as const;

export type DaemonSubscribeTopic = (typeof DAEMON_SUBSCRIBE_TOPICS)[number];

const DAEMON_SUBSCRIBE_TOPIC_SET: ReadonlySet<string> = new Set(DAEMON_SUBSCRIBE_TOPICS);

export function isDaemonSubscribeTopic(value: unknown): value is DaemonSubscribeTopic {
  return typeof value === "string" && DAEMON_SUBSCRIBE_TOPIC_SET.has(value);
}

export const PRELOAD_IPC_CHANNELS = {
${channelLines}
} as const satisfies Record<string, \`hoopoe.\${string}\`>;

export type PreloadIpcChannelKey = keyof typeof PRELOAD_IPC_CHANNELS;
export type PreloadIpcChannelValue =
  (typeof PRELOAD_IPC_CHANNELS)[PreloadIpcChannelKey];

const PRELOAD_IPC_CHANNEL_VALUES: ReadonlySet<string> = new Set(
  Object.values(PRELOAD_IPC_CHANNELS),
);

export function isPreloadIpcChannel(value: unknown): value is PreloadIpcChannelValue {
  return typeof value === "string" && PRELOAD_IPC_CHANNEL_VALUES.has(value);
}

export const PRELOAD_IPC_CHANNEL_CONTRACTS = {
${preloadContractLines}
} as const satisfies Record<
  string,
  {
    readonly channel: PreloadIpcChannelValue;
    readonly input: string;
    readonly output: string;
  }
>;

export const MOCK_FLYWHEEL_COMMANDS = {
${fmtObject(mockCommandEntries)}
} as const satisfies Record<string, \`mock-flywheel.\${string}\`>;

export type MockFlywheelCommandId =
  (typeof MOCK_FLYWHEEL_COMMANDS)[keyof typeof MOCK_FLYWHEEL_COMMANDS];

export const INTERNAL_IPC_COMMANDS = {
${fmtObject(internalCommandEntries)}
} as const satisfies Record<string, \`internal.\${string}\`>;

export type InternalIpcCommandId =
  | (typeof INTERNAL_IPC_COMMANDS)[keyof typeof INTERNAL_IPC_COMMANDS]
  | MockFlywheelCommandId;

const INTERNAL_IPC_COMMAND_VALUES: ReadonlySet<string> = new Set([
  ...Object.values(INTERNAL_IPC_COMMANDS),
  ...Object.values(MOCK_FLYWHEEL_COMMANDS),
]);

export function isInternalIpcCommand(value: unknown): value is InternalIpcCommandId {
  return typeof value === "string" && INTERNAL_IPC_COMMAND_VALUES.has(value);
}
`;
}

function usage(): string {
  return [
    "Usage: bun scripts/gen-preload-contract.ts [--out <path> | --stdout]",
    "",
    "Options:",
    "  --out <path>  Write generated TypeScript to the given path.",
    "  --stdout      Write generated TypeScript to stdout without touching files.",
    "  -h, --help    Print this help.",
  ].join("\n");
}

function parseArgs(args: readonly string[]): GeneratorOptions {
  const options: GeneratorOptions = {
    outPath: defaultOutPath,
    stdout: false,
  };
  let stdoutSeen = false;
  let outSeen = false;

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index] ?? "";
    if (arg === "--help" || arg === "-h") {
      process.stdout.write(`${usage()}\n`);
      process.exit(0);
    }
    if (arg === "--stdout") {
      if (stdoutSeen) {
        throw new Error("--stdout was provided multiple times");
      }
      stdoutSeen = true;
      if (outSeen) {
        throw new Error("--stdout cannot be combined with --out");
      }
      options.stdout = true;
      options.outPath = null;
      continue;
    }
    if (arg === "--out") {
      if (outSeen) {
        throw new Error("--out was provided multiple times");
      }
      outSeen = true;
      if (stdoutSeen) {
        throw new Error("--out cannot be combined with --stdout");
      }
      const next = args[index + 1];
      if (next === undefined || next.startsWith("--")) {
        throw new Error("--out requires a path argument");
      }
      if (next === "-") {
        throw new Error("--out requires a filesystem path; use --stdout for stdout");
      }
      options.outPath = resolve(process.cwd(), next);
      index += 1;
      continue;
    }
    throw new Error(`Unknown argument: ${arg}`);
  }

  return options;
}

function writeGeneratedOutput(ts: string, options: GeneratorOptions): string {
  if (options.stdout) {
    process.stdout.write(ts);
    return "stdout";
  }

  if (options.outPath === null) {
    throw new Error("Internal error: no output path configured");
  }

  mkdirSync(dirname(options.outPath), { recursive: true });
  writeFileSync(options.outPath, ts);
  return options.outPath;
}

const yamlText = readFileSync(yamlPath, "utf8");
const api = parseYaml(yamlText);
assertValidPreloadContracts(api);
const ts = generateTs(api);
const options = parseArgs(process.argv.slice(2));
const output = writeGeneratedOutput(ts, options);

if (!options.stdout) {
  process.stdout.write(
    `[gen-preload-contract] OK — wrote ${output} (${Object.keys(api.daemonRequestMethods).length} methods, ${Object.keys(api.daemonSubscribeTopics).length} topics, ${Object.keys(api.preloadChannels).length} channels, ${Object.keys(api.mockFlywheelCommands).length + Object.keys(api.internalCommands).length} internal commands)\n`,
  );
}
