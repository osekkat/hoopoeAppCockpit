// preload-api.test.ts — hp-rflj contract tests for preload-api.yaml.
//
// Asserts that the YAML parses with the expected high-level shape. Pinning
// counts catches accidental deletions and the round-trip catches accidental
// duplicates. The drift gate (validate-preload-codegen.ts) catches the YAML
// vs generated-TS mismatch separately.

import { expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const yamlPath = resolve(pkgRoot, "preload-api.yaml");
const generatorPath = resolve(pkgRoot, "scripts", "gen-preload-contract.ts");
const validatorPath = resolve(pkgRoot, "scripts", "validate-preload-codegen.ts");
const generatedPath = resolve(
  pkgRoot,
  "..",
  "..",
  "apps",
  "desktop",
  "src",
  "shared",
  "ipc-contract.gen.ts",
);

const yamlText = readFileSync(yamlPath, "utf8");
const generatedText = readFileSync(generatedPath, "utf8");

function generatedSnapshot(): { text: string; mtimeMs: number } {
  return {
    text: readFileSync(generatedPath, "utf8"),
    mtimeMs: statSync(generatedPath).mtimeMs,
  };
}

function runBunScript(args: string[], env?: NodeJS.ProcessEnv) {
  return spawnSync("bun", args, {
    cwd: pkgRoot,
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
    env: env === undefined ? process.env : env,
  });
}

test("preload-api.yaml header documents threat model + parity gate", () => {
  expect(yamlText).toContain("preload-api.yaml");
  expect(yamlText).toContain("renderer cannot reach");
  expect(yamlText.toLowerCase()).toContain("threat model");
  expect(yamlText).toContain("Adding a new method/topic/channel is a SECURITY-RELEVANT change");
});

test("preload-api.yaml schemaVersion is 1", () => {
  expect(yamlText).toMatch(/^schemaVersion:\s*1\s*$/m);
});

test("preload-api.yaml declares all sections in canonical order", () => {
  const idxMethods = yamlText.indexOf("\ndaemonRequestMethods:");
  const idxTopics = yamlText.indexOf("\ndaemonSubscribeTopics:");
  const idxChannels = yamlText.indexOf("\npreloadChannels:");
  const idxMockCommands = yamlText.indexOf("\nmockFlywheelCommands:");
  const idxInternalCommands = yamlText.indexOf("\ninternalCommands:");
  expect(idxMethods).toBeGreaterThan(0);
  expect(idxTopics).toBeGreaterThan(idxMethods);
  expect(idxChannels).toBeGreaterThan(idxTopics);
  expect(idxMockCommands).toBeGreaterThan(idxChannels);
  expect(idxInternalCommands).toBeGreaterThan(idxMockCommands);
});

test("generated TS contains all method names from YAML (no silent loss)", () => {
  // Spot-check the load-bearing identifiers. The full byte-level parity is
  // enforced by the validate-preload-codegen.ts drift gate.
  const required = [
    "ping", "health", "version", "capabilities",
    "auth.exchangePairingForBearer", "auth.issueWsToken",
    "settings.get", "settings.set",
    "projects.list",
    "projects.create", "projects.import", "projects.clone", "projects.readiness",
    "beads.get", "triage.get", "swarm.snapshot",
    "mail.dump", "reservations.list",
    "build-log.get", "pane-log.get",
    "approvals.list", "approvals.approve", "approvals.deny", "approvals.extend",
  ];
  for (const m of required) {
    expect(generatedText).toContain(`"${m}"`);
  }
});

test("generated TS contains all subscribe topics from YAML", () => {
  const required = [
    "events.swarm", "events.beads", "events.activity", "events.health",
    "events.tend", "events.approvals", "events.replay", "events.settings",
  ];
  for (const t of required) {
    expect(generatedText).toContain(`"${t}"`);
  }
});

test("generated TS preserves PRELOAD_IPC_CHANNELS satisfies constraint", () => {
  expect(generatedText).toContain("satisfies Record<string, `hoopoe.${string}`>");
  expect(generatedText).toContain("hoopoe.daemon.request");
  expect(generatedText).toContain("hoopoe.daemon.subscribe");
  expect(generatedText).toContain("hoopoe.daemon.unsubscribe");
});

test("generated TS preserves explicit internal command manifests", () => {
  expect(generatedText).toContain("MOCK_FLYWHEEL_COMMANDS");
  expect(generatedText).toContain("INTERNAL_IPC_COMMANDS");
  expect(generatedText).toContain('"mock-flywheel.health"');
  expect(generatedText).toContain('"mock-flywheel.beads.get"');
  expect(generatedText).toContain('"internal.schemas-smoke.project"');
  expect(generatedText).not.toContain("INTERNAL_IPC_COMMAND_PREFIXES");
});

test("generated TS carries the DO-NOT-EDIT marker", () => {
  expect(generatedText).toContain("GENERATED — DO NOT EDIT");
  expect(generatedText).toContain("packages/schemas/preload-api.yaml");
  expect(generatedText).toContain("packages/schemas/scripts/gen-preload-contract.ts");
});

test("gen-preload-contract --out writes only the requested file", () => {
  const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-preload-gen-test-"));
  try {
    const before = generatedSnapshot();
    const outPath = join(tmpDir, "ipc-contract.gen.ts");
    const result = runBunScript([generatorPath, "--out", outPath]);

    expect(result.status).toBe(0);
    expect(result.stdout).toContain(`[gen-preload-contract] OK — wrote ${outPath}`);
    expect(readFileSync(outPath, "utf8")).toBe(before.text);
    expect(generatedSnapshot()).toEqual(before);
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

test("gen-preload-contract rejects repeated output flags with precise errors", () => {
  const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-preload-gen-args-"));
  try {
    const before = generatedSnapshot();
    const cases = [
      {
        args: [generatorPath, "--stdout", "--stdout"],
        message: "--stdout was provided multiple times",
      },
      {
        args: [generatorPath, "--out", join(tmpDir, "first.ts"), "--out", join(tmpDir, "second.ts")],
        message: "--out was provided multiple times",
      },
    ];

    for (const { args, message } of cases) {
      const result = runBunScript(args);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain(message);
    }

    expect(generatedSnapshot()).toEqual(before);
    expect(existsSync(join(tmpDir, "first.ts"))).toBe(false);
    expect(existsSync(join(tmpDir, "second.ts"))).toBe(false);
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});

test("gen-preload-contract rejects --out dash with stdout guidance", () => {
  const before = generatedSnapshot();
  const result = runBunScript([generatorPath, "--out", "-"]);

  expect(result.status).toBe(1);
  expect(result.stderr).toContain("--out requires a filesystem path; use --stdout for stdout");
  expect(generatedSnapshot()).toEqual(before);
});

test("validate-preload-codegen is read-only on success", () => {
  const before = generatedSnapshot();
  const result = runBunScript([validatorPath]);

  expect(result.status).toBe(0);
  expect(result.stdout).toContain("[validate-preload-codegen] OK");
  expect(generatedSnapshot()).toEqual(before);
  expect(existsSync(`${generatedPath}.drift`)).toBe(false);
});

test("validate-preload-codegen is read-only when the generator fails", () => {
  const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-preload-failing-bun-"));
  try {
    const fakeBun = join(tmpDir, "bun");
    writeFileSync(fakeBun, "#!/bin/sh\necho fake generator failure >&2\nexit 42\n");
    chmodSync(fakeBun, 0o755);

    const before = generatedSnapshot();
    const result = spawnSync(process.execPath, [validatorPath], {
      cwd: pkgRoot,
      stdio: ["ignore", "pipe", "pipe"],
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${tmpDir}${delimiter}${process.env.PATH ?? ""}`,
      },
    });

    expect(result.status).toBe(1);
    expect(result.stderr).toContain("[validate-preload-codegen] generator failed");
    expect(result.stderr).toContain("fake generator failure");
    expect(generatedSnapshot()).toEqual(before);
    expect(existsSync(`${generatedPath}.drift`)).toBe(false);
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }
});
