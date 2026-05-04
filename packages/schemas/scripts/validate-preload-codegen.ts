#!/usr/bin/env bun
//
// validate-preload-codegen.ts — codegen drift gate for the preload IPC
// contract TS surface (hp-rflj).
//
// Re-runs `gen-preload-contract.ts` to a temp file, then byte-compares it
// against the committed `apps/desktop/src/shared/ipc-contract.gen.ts`.
// Exits non-zero on any difference so CI fails when a developer edits
// `preload-api.yaml` without regenerating the TS (or vice versa).
//
// Run locally: `bun run --cwd packages/schemas validate:preload`

import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const repoRoot = resolve(pkgRoot, "..", "..");
const generator = join(pkgRoot, "scripts", "gen-preload-contract.ts");
const committed = resolve(repoRoot, "apps/desktop/src/shared/ipc-contract.gen.ts");
const manual = resolve(repoRoot, "apps/desktop/src/shared/ipc-contract.ts");

type ContractModule = {
  DAEMON_REQUEST_METHODS: readonly string[];
  DAEMON_SUBSCRIBE_TOPICS: readonly string[];
  PRELOAD_IPC_CHANNELS: Record<string, string>;
  MOCK_FLYWHEEL_COMMANDS: Record<string, string>;
  INTERNAL_IPC_COMMANDS: Record<string, string>;
};

type ComparableContract = {
  daemonRequestMethods: readonly string[];
  daemonSubscribeTopics: readonly string[];
  preloadIpcChannels: Record<string, string>;
  mockFlywheelCommands: Record<string, string>;
  internalIpcCommands: Record<string, string>;
};

function stableObject(input: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(input).toSorted(([a], [b]) => a.localeCompare(b)));
}

function comparableContract(module: ContractModule): ComparableContract {
  return {
    daemonRequestMethods: [...module.DAEMON_REQUEST_METHODS],
    daemonSubscribeTopics: [...module.DAEMON_SUBSCRIBE_TOPICS],
    preloadIpcChannels: stableObject(module.PRELOAD_IPC_CHANNELS),
    mockFlywheelCommands: stableObject(module.MOCK_FLYWHEEL_COMMANDS),
    internalIpcCommands: stableObject(module.INTERNAL_IPC_COMMANDS),
  };
}

function assertContractModule(module: Partial<ContractModule>, label: string): ContractModule {
  const required = [
    "DAEMON_REQUEST_METHODS",
    "DAEMON_SUBSCRIBE_TOPICS",
    "PRELOAD_IPC_CHANNELS",
    "MOCK_FLYWHEEL_COMMANDS",
    "INTERNAL_IPC_COMMANDS",
  ] as const;
  for (const key of required) {
    if (!(key in module)) {
      throw new Error(`${label} is missing export ${key}`);
    }
  }
  return module as ContractModule;
}

async function importContract(path: string, label: string): Promise<ComparableContract> {
  const url = pathToFileURL(path);
  url.searchParams.set("validatePreloadCodegen", `${Date.now()}-${Math.random()}`);
  const module = assertContractModule(await import(url.href), label);
  return comparableContract(module);
}

function formatContractDiff(
  section: keyof ComparableContract,
  manualValue: ComparableContract[keyof ComparableContract],
  generatedValue: ComparableContract[keyof ComparableContract],
): string {
  return [
    `section: ${section}`,
    "manual ipc-contract.ts:",
    JSON.stringify(manualValue, null, 2),
    "generated ipc-contract.gen.ts:",
    JSON.stringify(generatedValue, null, 2),
  ].join("\n");
}

function contractParityDiffs(
  manualContract: ComparableContract,
  generatedContract: ComparableContract,
): string[] {
  const sections = [
    "daemonRequestMethods",
    "daemonSubscribeTopics",
    "preloadIpcChannels",
    "mockFlywheelCommands",
    "internalIpcCommands",
  ] as const satisfies readonly (keyof ComparableContract)[];

  const diffs: string[] = [];
  for (const section of sections) {
    const manualValue = manualContract[section];
    const generatedValue = generatedContract[section];
    if (JSON.stringify(manualValue) !== JSON.stringify(generatedValue)) {
      diffs.push(formatContractDiff(section, manualValue, generatedValue));
    }
  }
  return diffs;
}

if (!existsSync(committed)) {
  process.stderr.write(
    `[validate-preload-codegen] FAIL — committed file missing at ${committed}.\n` +
      "Fix: run `bun run --cwd packages/schemas generate:preload` and commit the result.\n",
  );
  process.exit(1);
}

if (!existsSync(manual)) {
  process.stderr.write(
    `[validate-preload-codegen] FAIL — manual file missing at ${manual}.\n` +
      "Fix: restore apps/desktop/src/shared/ipc-contract.ts or switch all imports to the generated contract.\n",
  );
  process.exit(1);
}

const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-preload-validate-"));
// The generator writes to a fixed absolute path under apps/desktop. To
// validate without touching the committed file, we save its current
// contents, run the generator (which overwrites), capture the fresh
// output, and restore the original from backup.
const backup = join(tmpDir, "ipc-contract.gen.ts.backup");
const freshGenerated = join(tmpDir, "ipc-contract.gen.ts");
copyFileSync(committed, backup);

try {
  const result = spawnSync("bun", [generator], {
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
  });
  if (result.status !== 0) {
    // Restore backup before exiting on generator error.
    copyFileSync(backup, committed);
    process.stderr.write(
      `[validate-preload-codegen] generator failed (exit ${result.status})\n` +
        `stdout:\n${result.stdout}\nstderr:\n${result.stderr}\n`,
    );
    process.exit(1);
  }

  const want = readFileSync(backup, "utf8");
  const got = readFileSync(committed, "utf8");
  writeFileSync(freshGenerated, got);
  // Restore committed file no matter what.
  copyFileSync(backup, committed);

  const manualContract = await importContract(manual, "apps/desktop/src/shared/ipc-contract.ts");
  const generatedContract = await importContract(
    freshGenerated,
    "freshly generated apps/desktop/src/shared/ipc-contract.gen.ts",
  );
  const parityDiffs = contractParityDiffs(manualContract, generatedContract);

  if (want === got && parityDiffs.length === 0) {
    process.stdout.write(
      "[validate-preload-codegen] OK — ipc-contract.gen.ts matches preload-api.yaml and ipc-contract.ts\n",
    );
    process.exit(0);
  }

  const messages: string[] = [];
  if (want !== got) {
    const driftPath = `${committed}.drift`;
    writeFileSync(driftPath, got);
    messages.push(
      "[validate-preload-codegen] DRIFT — preload-api.yaml and ipc-contract.gen.ts disagree.",
      `Fresh codegen written to ${driftPath} for inspection.`,
      "Fix: run `bun run --cwd packages/schemas generate:preload` and commit the result.",
    );
  }
  if (parityDiffs.length > 0) {
    messages.push(
      "[validate-preload-codegen] DRIFT — ipc-contract.ts and generated preload contract disagree.",
      ...parityDiffs,
      "Fix: update packages/schemas/preload-api.yaml, run `bun run --cwd packages/schemas generate:preload`, and reconcile apps/desktop/src/shared/ipc-contract.ts.",
    );
  }
  process.stderr.write(`${messages.join("\n")}\n`);
  process.exit(1);
} finally {
  rmSync(tmpDir, { recursive: true, force: true });
}
