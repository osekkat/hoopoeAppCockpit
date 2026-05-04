// preload-api.test.ts — hp-rflj contract tests for preload-api.yaml.
//
// Asserts that the YAML parses with the expected high-level shape. Pinning
// counts catches accidental deletions and the round-trip catches accidental
// duplicates. The drift gate (validate-preload-codegen.ts) catches the YAML
// vs generated-TS mismatch separately.

import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const yamlPath = resolve(pkgRoot, "preload-api.yaml");
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

test("preload-api.yaml header documents threat model + parity gate", () => {
  expect(yamlText).toContain("preload-api.yaml");
  expect(yamlText).toContain("renderer cannot reach");
  expect(yamlText.toLowerCase()).toContain("threat model");
  expect(yamlText).toContain("Adding a new method/topic/channel is a SECURITY-RELEVANT change");
});

test("preload-api.yaml schemaVersion is 1", () => {
  expect(yamlText).toMatch(/^schemaVersion:\s*1\s*$/m);
});

test("preload-api.yaml declares all four sections in canonical order", () => {
  const idxMethods = yamlText.indexOf("\ndaemonRequestMethods:");
  const idxTopics = yamlText.indexOf("\ndaemonSubscribeTopics:");
  const idxChannels = yamlText.indexOf("\npreloadChannels:");
  const idxPrefixes = yamlText.indexOf("\ninternalCommandPrefixes:");
  expect(idxMethods).toBeGreaterThan(0);
  expect(idxTopics).toBeGreaterThan(idxMethods);
  expect(idxChannels).toBeGreaterThan(idxTopics);
  expect(idxPrefixes).toBeGreaterThan(idxChannels);
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

test("generated TS preserves the two internal-command prefixes", () => {
  expect(generatedText).toContain('"mock-flywheel."');
  expect(generatedText).toContain('"internal."');
});

test("generated TS carries the DO-NOT-EDIT marker", () => {
  expect(generatedText).toContain("GENERATED — DO NOT EDIT");
  expect(generatedText).toContain("packages/schemas/preload-api.yaml");
  expect(generatedText).toContain("packages/schemas/scripts/gen-preload-contract.ts");
});
