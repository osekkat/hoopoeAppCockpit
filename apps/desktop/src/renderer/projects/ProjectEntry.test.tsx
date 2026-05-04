// hp-ilt — Phase 4 ProjectEntry render tests.
//
// Uses `react-dom/server` snapshot-style assertions (matching the rest of
// the renderer test surface in apps/desktop/src/renderer/shell.test.tsx)
// instead of pulling in @testing-library/react. Hooks that depend on a
// QueryClient are exercised through a minimal QueryClientProvider wrapper.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ProjectEntry, ReadinessPanel } from "./ProjectEntry.tsx";
import { ProjectsBridgeUnavailableError, type ReadinessOutput } from "./data.ts";

function withQueryClient(node: React.ReactNode): React.ReactNode {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

test("ProjectEntry: renders three tabs with import selected by default", () => {
  const markup = renderToStaticMarkup(withQueryClient(<ProjectEntry />));
  expect(markup).toContain("data-testid=\"project-entry\"");
  expect(markup).toContain("data-testid=\"project-entry-tab-import\"");
  expect(markup).toContain("data-testid=\"project-entry-tab-create\"");
  expect(markup).toContain("data-testid=\"project-entry-tab-clone\"");
  // Import is selected by default — its description text is visible.
  expect(markup).toContain("Point Hoopoe at a checkout that already lives on your VPS.");
  // Import form is the rendered tabpanel.
  expect(markup).toContain("data-testid=\"project-entry-import-form\"");
  expect(markup).not.toContain("data-testid=\"project-entry-create-form\"");
});

test("ProjectEntry: respects initialMode for opening on a different tab", () => {
  const markup = renderToStaticMarkup(withQueryClient(<ProjectEntry initialMode="create" />));
  expect(markup).toContain("data-testid=\"project-entry-create-form\"");
  expect(markup).not.toContain("data-testid=\"project-entry-import-form\"");
  expect(markup).toContain("aria-selected=\"true\"");
  expect(markup).toContain("Initialize a fresh repo on the VPS");
});

test("ProjectEntry: clone form exposes remote URL + parent dir fields", () => {
  const markup = renderToStaticMarkup(withQueryClient(<ProjectEntry initialMode="clone" />));
  expect(markup).toContain("data-testid=\"project-entry-clone-form\"");
  expect(markup).toContain("name=\"hh-clone-remote\"");
  expect(markup).toContain("name=\"hh-clone-parent\"");
  expect(markup).toContain("Clone project");
});

test("ProjectEntry: header surfaces the required-origin policy from §1.1", () => {
  const markup = renderToStaticMarkup(withQueryClient(<ProjectEntry />));
  expect(markup).toContain("Add a project to Hoopoe");
  expect(markup.toLowerCase()).toContain("external git remote is required");
  expect(markup).toContain("plan.md §1.1");
});

test("ReadinessPanel: renders nothing when bridge is unavailable", () => {
  const markup = renderToStaticMarkup(
    <ReadinessPanel data={undefined} error={new ProjectsBridgeUnavailableError()} isFetching={false} />,
  );
  expect(markup).toBe("");
});

test("ReadinessPanel: renders 'ready' state with all-satisfied requirements", () => {
  const data: ReadinessOutput = {
    gate: "imported",
    rootPath: "/data/projects/foo",
    satisfied: true,
    requirements: [
      { id: "git.present", label: "Git repository initialized", satisfied: true },
      {
        id: "git.origin",
        label: "origin remote configured",
        satisfied: true,
        note: "origin: git@github.com:org/repo.git",
      },
    ],
  };
  const markup = renderToStaticMarkup(
    <ReadinessPanel data={data} error={null} isFetching={false} />,
  );
  expect(markup).toContain("Ready to import");
  expect(markup).toContain("Git repository initialized");
  expect(markup).toContain("origin: git@github.com:org/repo.git");
  expect(markup).toContain("data-satisfied=\"true\"");
  // Per-requirement testids let the picker route assert specific gates.
  expect(markup).toContain("data-testid=\"readiness-git.present\"");
});

test("ReadinessPanel: renders missing-precondition list with notes", () => {
  const data: ReadinessOutput = {
    gate: "imported",
    rootPath: "/data/projects/bar",
    satisfied: false,
    requirements: [
      { id: "git.present", label: "Git repository initialized", satisfied: true },
      {
        id: "git.origin",
        label: "origin remote configured",
        satisfied: false,
        note: "v1 requires an external Git remote (plan.md §1.1)",
      },
      {
        id: "agents.md",
        label: "AGENTS.md present",
        satisfied: false,
        note: "create AGENTS.md so coding agents have project guidelines",
      },
    ],
  };
  const markup = renderToStaticMarkup(
    <ReadinessPanel data={data} error={null} isFetching={false} />,
  );
  expect(markup).toContain("Missing preconditions");
  expect(markup).toContain("v1 requires an external Git remote");
  expect(markup).toContain("create AGENTS.md so coding agents have project guidelines");
  expect(markup).toContain("data-satisfied=\"false\"");
});

test("ReadinessPanel: surfaces non-bridge errors", () => {
  const markup = renderToStaticMarkup(
    <ReadinessPanel data={undefined} error={new Error("readiness probe timed out")} isFetching={false} />,
  );
  expect(markup).toContain("Readiness probe failed");
  expect(markup).toContain("readiness probe timed out");
});
