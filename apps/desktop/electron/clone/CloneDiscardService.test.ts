// hp-58wp — CloneDiscardService tests.

import { describe, expect, test } from "bun:test";

import {
  CLEAN_CLONE_STATE,
  type CloneState,
  type DiscardLocalChangesResult,
} from "./index.ts";
import { CloneGitError } from "./git.ts";
import {
  CloneDiscardService,
  CloneDiscardServiceError,
  type CloneDiscardAuditEvent,
} from "./CloneDiscardService.ts";

interface Fixture {
  readonly service: CloneDiscardService;
  readonly auditEvents: CloneDiscardAuditEvent[];
  readonly engineCalls: Array<{ readonly cloneRepoPath: string }>;
}

interface FixtureOptions {
  readonly cloneState?: CloneState | null;
  readonly engineResult?: DiscardLocalChangesResult;
  readonly engineError?: Error;
  readonly resolverPath?: string;
  readonly auditThrows?: boolean;
}

function makeFixture(opts: FixtureOptions = {}): Fixture {
  const auditEvents: CloneDiscardAuditEvent[] = [];
  const engineCalls: Array<{ readonly cloneRepoPath: string }> = [];

  const service = new CloneDiscardService({
    resolveCloneRepoPath: (projectId) =>
      opts.resolverPath ?? `/fixture/projects/${projectId}/repo`,
    readCloneState: () => (opts.cloneState === undefined ? clonedState() : opts.cloneState),
    audit: (event) => {
      auditEvents.push(event);
      if (opts.auditThrows) throw new Error("audit sink boom");
    },
    engine: ({ cloneRepoPath }) => {
      engineCalls.push({ cloneRepoPath });
      if (opts.engineError) throw opts.engineError;
      return (
        opts.engineResult ?? {
          removedPathCount: 0,
          resetToSha: "0123456789abcdef0123456789abcdef01234567",
        }
      );
    },
    now: () => new Date("2026-05-04T01:00:00Z"),
  });

  return { service, auditEvents, engineCalls };
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
  test("happy path: invokes engine + emits ok audit + returns engine result", () => {
    const fx = makeFixture({
      engineResult: {
        removedPathCount: 3,
        resetToSha: "abcdef0123456789abcdef0123456789abcdef01",
      },
    });
    const result = fx.service.discardLocalChanges({ projectId: "demo-app" });

    expect(result.removedPathCount).toBe(3);
    expect(result.resetToSha).toBe("abcdef0123456789abcdef0123456789abcdef01");

    expect(fx.engineCalls).toEqual([{ cloneRepoPath: "/fixture/projects/demo-app/repo" }]);

    expect(fx.auditEvents.length).toBe(1);
    const event = fx.auditEvents[0]!;
    expect(event.kind).toBe("clone.discard-local-changes");
    expect(event.projectId).toBe("demo-app");
    expect(event.cloneRepoPath).toBe("/fixture/projects/demo-app/repo");
    expect(event.outcome).toBe("ok");
    expect(event.reasonCode).toBe("ok");
    expect(event.removedPathCount).toBe(3);
    expect(event.resetToSha).toBe("abcdef0123456789abcdef0123456789abcdef01");
    expect(event.at).toBe("2026-05-04T01:00:00.000Z");
  });

  test("rejects malformed projectId WITHOUT touching the engine", () => {
    const fx = makeFixture();
    expect(() =>
      fx.service.discardLocalChanges({ projectId: "../escape" }),
    ).toThrow(CloneDiscardServiceError);
    expect(fx.engineCalls).toEqual([]);
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
    expect(fx.engineCalls).toEqual([]);
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
    expect(fx.engineCalls).toEqual([]);
    expect(fx.auditEvents.length).toBe(1);
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.clone-empty");
  });

  test("git failure: emits failed audit, throws discard.git-failed with engineCode", () => {
    const fx = makeFixture({
      engineError: new CloneGitError("no_upstream", "no upstream", "fatal: ..."),
    });
    try {
      fx.service.discardLocalChanges({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(CloneDiscardServiceError);
      const svc = err as CloneDiscardServiceError;
      expect(svc.code).toBe("discard.git-failed");
      expect(svc.details.engineCode).toBe("no_upstream");
    }
    expect(fx.engineCalls.length).toBe(1);
    expect(fx.auditEvents.length).toBe(1);
    expect(fx.auditEvents[0]?.outcome).toBe("failed");
    expect(fx.auditEvents[0]?.reasonCode).toBe("discard.git-failed");
  });

  test("non-CloneGitError engine throw still emits failed audit (engineCode=unknown)", () => {
    const fx = makeFixture({ engineError: new Error("network blip") });
    try {
      fx.service.discardLocalChanges({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneDiscardServiceError).details.engineCode).toBe("unknown");
    }
    expect(fx.auditEvents[0]?.message).toBe("network blip");
  });

  test("audit fires regardless of outcome (Guardrail 10)", () => {
    // Guardrail 10: audit always records, even when the Activity panel
    // suppresses. Verify both the success and refused paths fire audit.
    const success = makeFixture();
    success.service.discardLocalChanges({ projectId: "demo-app" });
    expect(success.auditEvents.length).toBe(1);
    expect(success.auditEvents[0]?.outcome).toBe("ok");

    const refused = makeFixture({ cloneState: null });
    expect(() =>
      refused.service.discardLocalChanges({ projectId: "demo-app" }),
    ).toThrow();
    expect(refused.auditEvents.length).toBe(1);
    expect(refused.auditEvents[0]?.outcome).toBe("refused");
  });

  test("audit sink that throws does NOT mask the discard outcome", () => {
    const fx = makeFixture({
      auditThrows: true,
      engineResult: { removedPathCount: 0, resetToSha: null },
    });
    // Even though the audit sink throws, the service's caller still
    // gets the engine result (audit failure must not lose user work).
    const result = fx.service.discardLocalChanges({ projectId: "demo-app" });
    expect(result.removedPathCount).toBe(0);
    expect(fx.auditEvents.length).toBe(1);
  });

  test("project-id validator accepts hyphens, dots, underscores, and digits", () => {
    for (const id of ["demo", "demo-app", "demo_app.v2", "abc123", "X"]) {
      const fx = makeFixture();
      expect(() => fx.service.discardLocalChanges({ projectId: id })).not.toThrow();
      expect(fx.auditEvents[0]?.outcome).toBe("ok");
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
