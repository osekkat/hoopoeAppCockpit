// hp-5bhy — CloneActionsService tests.

import { describe, expect, test } from "bun:test";

import {
  CLEAN_CLONE_STATE,
  CloneStateError,
  type CloneCapConfig,
  type CloneState,
  DEFAULT_CLONE_CAPS,
} from "./index.ts";
import {
  CloneActionsService,
  CloneActionsServiceError,
  type CloneActionsAuditEvent,
  validateCaps,
} from "./CloneActionsService.ts";

function clonedState(overrides: Partial<CloneState> = {}): CloneState {
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
    ...overrides,
  };
}

interface ServiceFixture {
  readonly service: CloneActionsService;
  readonly audit: CloneActionsAuditEvent[];
  readonly revealCalls: string[];
  readonly terminalCalls: string[];
  readonly stateUpdates: Array<{ projectId: string; patched: CloneState }>;
}

interface FixtureOptions {
  readonly resolverPath?: string;
  readonly revealError?: Error;
  readonly terminalError?: Error;
  readonly stateError?: Error;
  /** Initial state seen by updater. Pass null to simulate "no state". */
  readonly initialState?: CloneState | null;
  readonly auditThrows?: boolean;
}

function makeService(opts: FixtureOptions = {}): ServiceFixture {
  const audit: CloneActionsAuditEvent[] = [];
  const revealCalls: string[] = [];
  const terminalCalls: string[] = [];
  const stateUpdates: Array<{ projectId: string; patched: CloneState }> = [];
  const initial = opts.initialState === undefined ? clonedState() : opts.initialState;

  const service = new CloneActionsService({
    resolveCloneRepoPath: (projectId) =>
      opts.resolverPath ?? `/fixture/projects/${projectId}/repo`,
    revealInFinder: (path) => {
      revealCalls.push(path);
      if (opts.revealError) throw opts.revealError;
    },
    openInTerminal: (path) => {
      terminalCalls.push(path);
      if (opts.terminalError) throw opts.terminalError;
    },
    updateCloneState: (projectId, patcher) => {
      if (opts.stateError) throw opts.stateError;
      if (initial === null) {
        throw new CloneStateError("missing_state", `cannot update unknown clone state for project ${projectId}`);
      }
      const next = patcher(initial);
      stateUpdates.push({ projectId, patched: next });
      return next;
    },
    audit: (event) => {
      audit.push(event);
      if (opts.auditThrows) throw new Error("audit sink boom");
    },
    now: () => new Date("2026-05-04T01:00:00Z"),
  });

  return { service, audit, revealCalls, terminalCalls, stateUpdates };
}

describe("CloneActionsService.revealInFinder", () => {
  test("happy path: invokes finder revealer with cloneRepoPath + ok audit", async () => {
    const fx = makeService();
    await fx.service.revealInFinder({ projectId: "demo-app" });
    expect(fx.revealCalls).toEqual(["/fixture/projects/demo-app/repo"]);
    expect(fx.audit.length).toBe(1);
    expect(fx.audit[0]?.action).toBe("reveal-in-finder");
    expect(fx.audit[0]?.outcome).toBe("ok");
    expect(fx.audit[0]?.cloneRepoPath).toBe("/fixture/projects/demo-app/repo");
  });

  test("revealer throw: emits failed audit + throws actions.reveal-failed", async () => {
    const fx = makeService({ revealError: new Error("Finder unavailable") });
    try {
      await fx.service.revealInFinder({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect(err).toBeInstanceOf(CloneActionsServiceError);
      expect((err as CloneActionsServiceError).code).toBe("actions.reveal-failed");
    }
    expect(fx.audit[0]?.outcome).toBe("failed");
    expect(fx.audit[0]?.message).toBe("Finder unavailable");
  });

  test("invalid projectId: refused before invoking revealer", async () => {
    const fx = makeService();
    try {
      await fx.service.revealInFinder({ projectId: "../escape" });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.projectId-invalid");
    }
    expect(fx.revealCalls).toEqual([]);
    expect(fx.audit[0]?.outcome).toBe("refused");
    expect(fx.audit[0]?.action).toBe("reveal-in-finder");
  });
});

describe("CloneActionsService.openInTerminal", () => {
  test("happy path: invokes terminal opener + ok audit", async () => {
    const fx = makeService();
    await fx.service.openInTerminal({ projectId: "demo-app" });
    expect(fx.terminalCalls).toEqual(["/fixture/projects/demo-app/repo"]);
    expect(fx.audit[0]?.action).toBe("open-in-terminal");
    expect(fx.audit[0]?.outcome).toBe("ok");
  });

  test("terminal opener throw: emits failed audit + throws actions.terminal-failed", async () => {
    const fx = makeService({ terminalError: new Error("Terminal.app missing") });
    try {
      await fx.service.openInTerminal({ projectId: "demo-app" });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.terminal-failed");
    }
    expect(fx.audit[0]?.outcome).toBe("failed");
  });

  test("invalid projectId: refused before invoking terminal", async () => {
    const fx = makeService();
    try {
      await fx.service.openInTerminal({ projectId: "  " });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.projectId-invalid");
    }
    expect(fx.terminalCalls).toEqual([]);
  });
});

describe("CloneActionsService.setCapOverride", () => {
  test("happy path: writes patched state + emits ok audit", () => {
    const fx = makeService();
    const next = fx.service.setCapOverride({
      projectId: "demo-app",
      capsOverride: { softCapBytes: 1024 * 1024 * 1024, hardCapBytes: 4 * 1024 * 1024 * 1024 },
    });
    expect(next.capsOverride?.softCapBytes).toBe(1024 * 1024 * 1024);
    expect(fx.stateUpdates.length).toBe(1);
    expect(fx.audit[0]?.action).toBe("set-cap-override");
    expect(fx.audit[0]?.outcome).toBe("ok");
    expect(fx.audit[0]?.capsOverride?.hardCapBytes).toBe(4 * 1024 * 1024 * 1024);
  });

  test("clearing override (null) writes null + emits ok audit", () => {
    const fx = makeService({
      initialState: clonedState({ capsOverride: { softCapBytes: 1, hardCapBytes: 2 } }),
    });
    const next = fx.service.setCapOverride({ projectId: "demo-app", capsOverride: null });
    expect(next.capsOverride).toBeNull();
    expect(fx.audit[0]?.capsOverride).toBeNull();
  });

  test("rejects soft cap below minimum (refused, no state write)", () => {
    const fx = makeService();
    try {
      fx.service.setCapOverride({
        projectId: "demo-app",
        capsOverride: { softCapBytes: 1024, hardCapBytes: 2048 },
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.caps-invalid");
    }
    expect(fx.stateUpdates).toEqual([]);
    expect(fx.audit[0]?.outcome).toBe("refused");
  });

  test("rejects hard cap above maximum", () => {
    const fx = makeService();
    try {
      fx.service.setCapOverride({
        projectId: "demo-app",
        capsOverride: { softCapBytes: 64 * 1024 * 1024, hardCapBytes: 9999 * 1024 * 1024 * 1024 },
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.caps-invalid");
    }
    expect(fx.stateUpdates).toEqual([]);
  });

  test("rejects hard <= soft", () => {
    const fx = makeService();
    try {
      fx.service.setCapOverride({
        projectId: "demo-app",
        capsOverride: { softCapBytes: 100 * 1024 * 1024, hardCapBytes: 100 * 1024 * 1024 },
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.caps-invalid");
    }
  });

  test("missing_state from updater is mapped to clone-state-missing refused", () => {
    const fx = makeService({ initialState: null });
    try {
      fx.service.setCapOverride({
        projectId: "demo-app",
        capsOverride: DEFAULT_CLONE_CAPS,
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.clone-state-missing");
    }
    expect(fx.audit[0]?.outcome).toBe("refused");
    expect(fx.audit[0]?.reasonCode).toBe("actions.clone-state-missing");
  });

  test("non-CloneStateError updater failure → cap-write-failed", () => {
    const fx = makeService({ stateError: new Error("disk full") });
    try {
      fx.service.setCapOverride({
        projectId: "demo-app",
        capsOverride: DEFAULT_CLONE_CAPS,
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.cap-write-failed");
    }
    expect(fx.audit[0]?.outcome).toBe("failed");
    expect(fx.audit[0]?.message).toBe("disk full");
  });

  test("invalid projectId is refused before validation/state read", () => {
    const fx = makeService();
    try {
      fx.service.setCapOverride({
        projectId: "demo/app",
        capsOverride: DEFAULT_CLONE_CAPS,
      });
      throw new Error("expected throw");
    } catch (err) {
      expect((err as CloneActionsServiceError).code).toBe("actions.projectId-invalid");
    }
    expect(fx.stateUpdates).toEqual([]);
  });
});

describe("CloneActionsService — Guardrail-10 audit-always", () => {
  test("audit fires for every action regardless of outcome", async () => {
    // Three actions × ok + refused + failed = audit on every shape.
    const ok = makeService();
    await ok.service.revealInFinder({ projectId: "demo-app" });
    expect(ok.audit[0]?.outcome).toBe("ok");

    const refused = makeService();
    try {
      await refused.service.openInTerminal({ projectId: "../bad" });
    } catch {
      // expected
    }
    expect(refused.audit[0]?.outcome).toBe("refused");

    const failed = makeService({ revealError: new Error("boom") });
    try {
      await failed.service.revealInFinder({ projectId: "demo-app" });
    } catch {
      // expected
    }
    expect(failed.audit[0]?.outcome).toBe("failed");
  });

  test("audit-sink that throws does not mask the action outcome", async () => {
    const fx = makeService({ auditThrows: true });
    // The reveal call should still complete successfully even though the
    // audit sink throws.
    await fx.service.revealInFinder({ projectId: "demo-app" });
    expect(fx.revealCalls.length).toBe(1);
    expect(fx.audit.length).toBe(1);
  });
});

describe("validateCaps", () => {
  test("rejects non-finite numbers", () => {
    expect(validateCaps({ softCapBytes: NaN, hardCapBytes: 5 } as unknown as CloneCapConfig)).toContain("finite");
    expect(validateCaps({ softCapBytes: 5, hardCapBytes: Infinity } as unknown as CloneCapConfig)).toContain("finite");
  });

  test("returns null for sane defaults", () => {
    expect(validateCaps(DEFAULT_CLONE_CAPS)).toBeNull();
  });
});
