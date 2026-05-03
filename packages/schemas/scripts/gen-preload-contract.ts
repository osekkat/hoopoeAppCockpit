#!/usr/bin/env bun
//
// gen-preload-contract.ts — regenerate the renderer ↔ preload ↔ main IPC
// contract TS module from `packages/schemas/preload-api.yaml`.
//
// Output: `apps/desktop/src/shared/ipc-contract.gen.ts`
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

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const repoRoot = resolve(pkgRoot, "..", "..");
const yamlPath = resolve(pkgRoot, "preload-api.yaml");
const outPath = resolve(repoRoot, "apps/desktop/src/shared/ipc-contract.gen.ts");

interface PreloadApi {
  schemaVersion: number;
  daemonRequestMethods: Record<string, { description: string; bead: string }>;
  daemonSubscribeTopics: Record<string, { description: string; bead: string }>;
  preloadChannels: Record<string, string>;
  internalCommandPrefixes: string[];
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
    internalCommandPrefixes: [],
  };

  type Section =
    | "none"
    | "daemonRequestMethods"
    | "daemonSubscribeTopics"
    | "preloadChannels"
    | "internalCommandPrefixes";
  let section: Section = "none";
  let currentKey: string | null = null;
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
      continue;
    }
    if (/^internalCommandPrefixes:\s*$/.test(line)) {
      flushPending();
      section = "internalCommandPrefixes";
      continue;
    }
    if (/^[a-zA-Z]/.test(line)) {
      // New top-level — leaving the current section.
      flushPending();
      section = "none";
      continue;
    }

    if (section === "internalCommandPrefixes") {
      const m = /^\s*-\s+(\S+)\s*$/.exec(line);
      if (m && m[1] !== undefined) result.internalCommandPrefixes.push(m[1]);
      continue;
    }

    if (section === "preloadChannels") {
      const m = /^\s+([a-zA-Z]\w*):\s+(\S+)\s*$/.exec(line);
      if (m && m[1] !== undefined && m[2] !== undefined) {
        result.preloadChannels[m[1]] = m[2];
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

function generateTs(api: PreloadApi): string {
  const requestMethodNames = Object.keys(api.daemonRequestMethods);
  const subscribeTopicNames = Object.keys(api.daemonSubscribeTopics);
  const channelEntries = Object.entries(api.preloadChannels);
  const prefixes = api.internalCommandPrefixes;

  const fmtList = (xs: readonly string[]): string =>
    xs.map((x) => `  ${JSON.stringify(x)},`).join("\n");

  const channelLines = channelEntries
    .map(([key, value]) => `  ${key}: ${JSON.stringify(value)},`)
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

export const INTERNAL_IPC_COMMAND_PREFIXES = [
${fmtList(prefixes)}
] as const;

export type InternalIpcCommandPrefix = (typeof INTERNAL_IPC_COMMAND_PREFIXES)[number];
`;
}

const yamlText = readFileSync(yamlPath, "utf8");
const api = parseYaml(yamlText);
const ts = generateTs(api);
writeFileSync(outPath, ts);

process.stdout.write(
  `[gen-preload-contract] OK — wrote ${outPath} (${Object.keys(api.daemonRequestMethods).length} methods, ${Object.keys(api.daemonSubscribeTopics).length} topics, ${Object.keys(api.preloadChannels).length} channels, ${api.internalCommandPrefixes.length} prefixes)\n`,
);
