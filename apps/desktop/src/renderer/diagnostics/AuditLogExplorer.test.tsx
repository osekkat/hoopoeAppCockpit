import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AuditLogExplorer } from "./AuditLogExplorer.tsx";
import { createFixtureAuditEntries } from "./audit-log-model.ts";

test("AuditLogExplorer renders filters, timeline, detail, and redaction markers", () => {
  const entries = createFixtureAuditEntries("local-demo").filter((entry) => entry.id === "audit-006");
  const markup = renderToStaticMarkup(
    <AuditLogExplorer
      entries={entries}
      projectId="local-demo"
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
});
