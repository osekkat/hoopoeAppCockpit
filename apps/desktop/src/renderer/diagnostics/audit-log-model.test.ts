import { expect, test } from "bun:test";
import {
  auditCorrelationChain,
  buildAuditExplorerModel,
  createFixtureAuditEntries,
  defaultAuditFilters,
} from "./audit-log-model.ts";

const now = new Date("2026-05-04T08:00:00.000Z");

test("audit explorer model filters by actor and text query", () => {
  const entries = createFixtureAuditEntries("local-demo");
  const model = buildAuditExplorerModel(entries, {
    ...defaultAuditFilters,
    actorKinds: ["agent"],
    query: "BlueHill",
  }, now);

  expect(model.filteredEntries.map((entry) => entry.id)).toEqual(["audit-002"]);
  expect(model.groups[0]?.label).toContain("corr-swarm-1");
  expect(model.selectedEntry?.actor.displayName).toBe("BlueHill");
});

test("audit explorer model keeps secret values out of searchable fixtures", () => {
  const entries = createFixtureAuditEntries("local-demo");
  const rawSecretSearch = buildAuditExplorerModel(entries, {
    ...defaultAuditFilters,
    query: "sk-live-",
  }, now);
  const redactedSearch = buildAuditExplorerModel(entries, {
    ...defaultAuditFilters,
    query: "[REDACTED:bearer-token]",
  }, now);

  expect(rawSecretSearch.filteredEntries).toHaveLength(0);
  expect(redactedSearch.filteredEntries.map((entry) => entry.id)).toEqual(["audit-006"]);
});

test("audit explorer model returns ordered correlation chains", () => {
  const entries = createFixtureAuditEntries("local-demo");
  const chain = auditCorrelationChain(entries, "corr-auth-rotation");

  expect(chain.map((entry) => entry.id)).toEqual(["audit-004", "audit-005", "audit-006"]);
  expect(chain[0]?.causationId).toBeUndefined();
  expect(chain[2]?.causationId).toBe("audit-005");
});

test("audit explorer export preview tracks filtered entry count", () => {
  const entries = createFixtureAuditEntries("local-demo");
  const all = buildAuditExplorerModel(entries, defaultAuditFilters, now);
  const approvals = buildAuditExplorerModel(entries, {
    ...defaultAuditFilters,
    categories: ["approval"],
  }, now);

  expect(all.exportPreview.totalEntries).toBe(10);
  expect(approvals.exportPreview.totalEntries).toBe(2);
  expect(approvals.exportPreview.fileName).toBe("audit-slice-20260504T080000Z.json");
  expect(approvals.exportPreview.fingerprint).toMatch(/^fnv32:[0-9a-f]{8}$/);
  expect(approvals.exportPreview).not.toHaveProperty("sha256");
});

test("audit explorer model default clock follows the current wall clock", () => {
  const fixture = createFixtureAuditEntries("local-demo")[0];
  if (!fixture) throw new Error("fixture audit entry missing");
  const recentEntry = {
    ...fixture,
    id: "audit-recent",
    seq: 99,
    timestamp: new Date(Date.now() - 60_000).toISOString(),
    summary: "Recent production audit event",
  };

  const model = buildAuditExplorerModel([recentEntry], defaultAuditFilters);

  expect(model.filteredEntries.map((entry) => entry.id)).toEqual(["audit-recent"]);
  expect(model.emptyReason).toBeNull();
});
