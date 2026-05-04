import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AuditLogExplorer } from "./AuditLogExplorer.tsx";
import { DiagnosticsStage } from "./DiagnosticsStage.tsx";
import { createFixtureAuditEntries } from "./audit-log-model.ts";

const fixedNow = new Date("2026-05-04T08:00:00.000Z");

test("AuditLogExplorer renders filters, timeline, detail, and redaction markers", () => {
  const entries = createFixtureAuditEntries("local-demo").filter((entry) => entry.id === "audit-006");
  const markup = renderToStaticMarkup(
    <AuditLogExplorer
      entries={entries}
      now={fixedNow}
    />,
  );

  expect(markup).toContain('data-testid="audit-log-explorer"');
  expect(markup).toContain("Audit log explorer");
  expect(markup).toContain("Search");
  expect(markup).toContain("Correlation");
  expect(markup).toContain("corr-auth-rotation");
  expect(markup).toContain("Redacted");
  expect(markup).toContain("[REDACTED:bearer-token]");
  expect(markup).toContain("audit-slice-20260504T080000Z.json");
  expect(markup).toContain("fingerprint fnv32:");
  expect(markup).not.toContain("sha256");
});

test("AuditLogExplorer without entries renders a real empty state, not fixture audit history", () => {
  const markup = renderToStaticMarkup(<AuditLogExplorer />);

  expect(markup).toContain('data-testid="audit-log-empty"');
  expect(markup).toContain("No audit entries");
  expect(markup).toContain("0 of 0 entries");
  expect(markup).not.toContain("Bearer secret rotated and stale sessions revoked");
  expect(markup).not.toContain("corr-auth-rotation");
});

test("DiagnosticsStage does not leak fixture audit entries when daemon data is absent", () => {
  const markup = renderToStaticMarkup(<DiagnosticsStage projectId="local-demo" />);

  expect(markup).toContain('data-testid="diagnostics-audit-stage"');
  expect(markup).toContain("No audit entries");
  expect(markup).not.toContain("BlueHill claimed hp-k6r");
  expect(markup).not.toContain("auth.secret_rotated");
});
