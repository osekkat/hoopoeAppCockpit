// hp-58wp/hp-hde4 — CloneDiscardService tests.

import { describe, expect, test } from "bun:test";

import {
  CLEAN_CLONE_STATE,
  type CloneState,
} from "./index.ts";
import {
  CloneDiscardService,
  CloneDiscardServiceError,
  type CloneDiscardAuditEvent,
} from "./CloneDiscardService.ts";

interface Fixture {
  readonly service: CloneDiscardService;
  readonly auditEvents: CloneDiscardAuditEvent[];
}

interface FixtureOptions {
  readonly cloneState?: CloneState | null;
  readonly resolverPath?: string;
  readonly auditThrows?: boolean;
}

function makeFixture(opts: FixtureOptions = {}): Fixture {
  const auditEvents: CloneDiscardAuditEvent[] = [];

  const service = new CloneDiscardService({
    resolveCloneRepoPath: (projectId) =>
      opts.resolverPath ?? `/fixture/projects/${projectId}/repo`,
    readCloneState: () => (opts.cloneState === undefined ? clonedState() : opts.cloneState),
    audit: (event) => {
      auditEvents.push(event);
      if (opts.auditThrows) throw new Error("audit sink boom");
    },
    now: () => new Date("2026-05-04T01:00:00Z"),
  });

  return { service, auditEvents };
}

function clonedState(): CloneState {
  return {
    projectId: "demo-app",
    originRemote: "git@github.com:org/demo.git",
    branch: "main",
    lastFetchedSha: "deadbeef0000000000000000000000000000beef",
    syncStatus: "synced",
    sizeBytes: 1024,
    lastSyncedAt: "2026-05-04T00:30:00Z",
    lastAccessedAt: "2026-05-04T00:45:00Z",
    lastError: null,
    capsOverride: null,
    dirtyState: CLEAN_CLONE_STATE,
  };
}

describe("CloneDiscardService.discardLocalChanges", () => {
  test("valid clone: refuses with read-only-mirror audit and explicit error", () => {
    const fx = makeFixture();
    try {
      fx.service.discardLocalChanges({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(CloneDiscardServiceError);
      const svc = err as CloneDiscardServiceError;
      expect(svc.code).toBe("discard.read-only-mirror");
      expect(svc.message).toContain("read-only");
      expect(svc.details.cloneRepoPath).toBe("/fixture/projects/demo-app/repo");
    }
    expect(fx.auditEvents.length).toBe(1);
    const event = fx.auditEvents[0]!;
    expect(event.kind).toBe("clone.discard-local-changes");
    expect(event.projectId).toBe("demo-app");
    expect(event.cloneRepoPath).toBe("/fixture/projects/demo-app/repo");
    expect(event.outcome).toBe("refused");
    expect(event.reasonCode).toBe("discard.read-only-mirror");
    expect(event.message).toContain("read-only");
    expect(event.at).toBe("2026-05-04T01:00:00.000Z");
  });

  test("rejects malformed projectId before resolving a mirror", () => {
    const fx = makeFixture();
    expect(() =>
      fx.service.discardLocalChanges({ projectId: "../escape" }),
    ).toThrow(CloneDiscardServiceError);
    expect(fx.auditEvents.length).toBe(1);
    expect(fx.auditEvents[0]?.outcome).toBe("refused");
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.projectId-invalid");
  });

  test("non-string projectId is refused (defense-in-depth past the type system)", () => {
    const fx = makeFixture();
    expect(() =>
      fx.service.discardLocalChanges({
        projectId: 42 as unknown as string,
      }),
    ).toThrow(CloneDiscardServiceError);
    expect(fx.auditEvents[0]?.outcome).toBe("refused");
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.projectId-invalid");
  });

  test("refuses when no clone-state.json exists for the project", () => {
    const fx = makeFixture({ cloneState: null });
    try {
      fx.service.discardLocalChanges({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(CloneDiscardServiceError);
      expect((err as CloneDiscardServiceError).code).toBe("discard.clone-not-cloned");
    }
    expect(fx.auditEvents.length).toBe(1);
    expect(fx.auditEvents[0]?.outcome).toBe("refused");
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.clone-not-cloned");
  });

  test("refuses when clone-state reports an empty/uncloned project", () => {
    const empty: CloneState = {
      ...clonedState(),
      lastFetchedSha: null,
      syncStatus: "uncloned",
      sizeBytes: 0,
    };
    const fx = makeFixture({ cloneState: empty });
    try {
      fx.service.discardLocalChanges({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(CloneDiscardServiceError);
      expect((err as CloneDiscardServiceError).code).toBe("discard.clone-empty");
    }
    expect(fx.auditEvents.length).toBe(1);
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.clone-empty");
  });

  test("audit fires regardless of outcome (Guardrail 10)", () => {
    const readOnly = makeFixture();
    expect(() =>
      readOnly.service.discardLocalChanges({ projectId: "demo-app" }),
    ).toThrow();
    expect(readOnly.auditEvents.length).toBe(1);
    expect(readOnly.auditEvents[0]?.outcome).toBe("refused");

    const refused = makeFixture({ cloneState: null });
    expect(() =>
      refused.service.discardLocalChanges({ projectId: "demo-app" }),
    ).toThrow();
    expect(refused.auditEvents.length).toBe(1);
    expect(refused.auditEvents[0]?.outcome).toBe("refused");
  });

  test("audit sink that throws does NOT mask the discard outcome", () => {
    const fx = makeFixture({ auditThrows: true });
    expect(() =>
      fx.service.discardLocalChanges({ projectId: "demo-app" }),
    ).toThrow(CloneDiscardServiceError);
    expect(fx.auditEvents.length).toBe(1);
  });

  test("project-id validator accepts hyphens, dots, underscores, and digits", () => {
    for (const id of ["demo", "demo-app", "demo_app.v2", "abc123", "X"]) {
      const fx = makeFixture();
      expect(() =>
        fx.service.discardLocalChanges({ projectId: id }),
      ).toThrow(CloneDiscardServiceError);
      expect(fx.auditEvents[0]?.outcome).toBe("refused");
      expect(fx.auditEvents[0]?.reasonCode).toBe("discard.read-only-mirror");
    }
  });

  test("project-id validator refuses traversal, slashes, control chars, leading dot", () => {
    for (const bad of ["../escape", ".hidden", "demo/app", "demo\nrm", "demo;rm", ""]) {
      const fx = makeFixture();
      expect(() => fx.service.discardLocalChanges({ projectId: bad })).toThrow();
      expect(fx.auditEvents[0]?.reasonCode).toBe("discard.projectId-invalid");
    }
  });
});
