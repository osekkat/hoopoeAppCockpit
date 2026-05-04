import { describe, expect, test } from "bun:test";
import {
  decideFeature,
  determineFeature,
  emptyRegistry,
  FEATURE_CATALOG,
  isToolId,
  lookupCapability,
  lookupCapabilityStatus,
  renderBucketFor,
  toolFromCapRef,
} from "./index.ts";
import type {
  CapabilityRegistry,
  FeatureCapabilityRequirement,
} from "./index.ts";

const REGISTRY_FIXTURE: CapabilityRegistry = {
  schemaVersion: 1,
  snapshotAt: "2026-05-02T23:29:34Z",
  daemonApiVersion: "0.1.0",
  fixturesVersion: "phase0-test",
  tools: {
    git: {
      tool: "git",
      version: "2.40.0",
      source: "CLI",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "git.status.read": { status: "ok" },
        "git.diff.read": { status: "ok" },
        "git.push": { status: "blocked-by-policy", notes: "snapshot scripts never push" },
      },
    },
    br: {
      tool: "br",
      version: "0.5.0",
      source: "CLI",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "br.issues.read": { status: "degraded", fallback: "text-parse" },
      },
    },
    bv: {
      tool: "bv",
      version: "1.0.0",
      source: "CLI",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "bv.robot.triage": { status: "ok" },
      },
    },
    dcg: {
      tool: "dcg",
      version: "1.0.0",
      source: "CLI",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "dcg.verdicts.subscribe": { status: "untested", notes: "format pinned via --help" },
      },
    },
    ntm: {
      tool: "ntm",
      version: "1.0.0",
      source: "ntm serve",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "ntm.sessions.list": { status: "ok" },
        "ntm.robot.snapshot": { status: "ok" },
        "ntm.panes.stream": { status: "missing", fallback: "tmux capture-pane" },
      },
    },
    agent_mail: {
      tool: "agent_mail",
      version: "1.0.0",
      source: "MCP",
      lastCheckedAt: "2026-05-02T23:29:34Z",
      fixturesVersion: "phase0-test",
      capabilities: {
        "agent_mail.messages.send": { status: "ok" },
        "agent_mail.reservations.list": { status: "ok" },
      },
    },
  },
};

describe("isToolId", () => {
  test("accepts every closed tool", () => {
    for (const id of [
      "ntm",
      "br",
      "bv",
      "agent_mail",
      "git",
      "ru",
      "caam",
      "caut",
      "dcg",
      "casr",
      "pt",
      "srp",
      "sbh",
      "ubs",
      "jsm",
      "jfp",
      "oracle",
      "rch",
      "rano",
      "health_ts",
      "health_py",
      "health_rs",
      "health_go",
      "health_generic",
    ]) {
      expect(isToolId(id)).toBe(true);
    }
  });

  test("rejects unknown tool ids", () => {
    expect(isToolId("kubelet")).toBe(false);
    expect(isToolId("")).toBe(false);
    expect(isToolId("health_")).toBe(false);
    expect(isToolId("health_unknown")).toBe(false);
  });
});

describe("toolFromCapRef", () => {
  test("extracts the tool prefix and validates it", () => {
    expect(toolFromCapRef("git.status.read")).toBe("git");
    expect(toolFromCapRef("agent_mail.messages.send")).toBe("agent_mail");
    expect(toolFromCapRef("br.issues.read")).toBe("br");
  });

  test("returns null for malformed refs", () => {
    expect(toolFromCapRef("git")).toBeNull();
    expect(toolFromCapRef("git.")).toBeNull();
    expect(toolFromCapRef(".read")).toBeNull();
    expect(toolFromCapRef("")).toBeNull();
  });

  test("rejects unknown tool prefixes", () => {
    expect(toolFromCapRef("kubelet.foo")).toBeNull();
  });
});

describe("lookupCapability / lookupCapabilityStatus", () => {
  test("preserves the full capId as map key", () => {
    const cap = lookupCapability(REGISTRY_FIXTURE, "git.status.read");
    expect(cap?.status).toBe("ok");
  });

  test("returns missing for unknown tool", () => {
    expect(lookupCapabilityStatus(REGISTRY_FIXTURE, "kubelet.x")).toBe("missing");
  });

  test("returns missing for unknown capId on a known tool", () => {
    expect(lookupCapabilityStatus(REGISTRY_FIXTURE, "git.unicorn")).toBe("missing");
  });

  test("returns missing on empty registry", () => {
    expect(lookupCapabilityStatus(emptyRegistry(), "git.status.read")).toBe("missing");
  });

  test("preserves Capability fields", () => {
    const cap = lookupCapability(REGISTRY_FIXTURE, "ntm.panes.stream");
    expect(cap).toEqual({ status: "missing", fallback: "tmux capture-pane" });
  });
});

describe("determineFeature", () => {
  test("blocked-by-policy outranks missing/degraded", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "swarm.bead.push-branch",
      capabilitiesRequired: ["git.status.read", "git.push"],
      capabilitiesOptional: [],
      degradedMode: {
        ifMissingRequired: "block_job",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "activity_panel_warning",
      },
    });
    expect(dec.render).toBe("blocked-by-policy");
    expect(dec.blockedByPolicy).toEqual(["git.push"]);
    expect(dec.contractAction).toBe("block_job");
  });

  test("missing required → unavailable", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "test.unknown",
      capabilitiesRequired: ["ntm.swarm.halt"],
      capabilitiesOptional: [],
      degradedMode: {
        ifMissingRequired: "emit_diagnostic",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "diagnostics_only",
      },
    });
    expect(dec.render).toBe("unavailable");
    expect(dec.missingRequired).toEqual(["ntm.swarm.halt"]);
  });

  test("untested treated as missing for required", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "approvals.dcg.subscribe",
      capabilitiesRequired: ["dcg.verdicts.subscribe"],
      capabilitiesOptional: [],
      degradedMode: {
        ifMissingRequired: "emit_diagnostic",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "diagnostics_only",
      },
    });
    expect(dec.render).toBe("unavailable");
    expect(dec.missingRequired).toEqual(["dcg.verdicts.subscribe"]);
  });

  test("required degraded → degraded render", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "test.degraded",
      capabilitiesRequired: ["br.issues.read"],
      capabilitiesOptional: [],
      degradedMode: {
        ifMissingRequired: "run_read_only",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "activity_panel_warning",
      },
    });
    expect(dec.render).toBe("degraded");
    expect(dec.degradedReasons).toEqual(["br.issues.read"]);
  });

  test("optional missing reported but render stays available", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "test.optional-missing",
      capabilitiesRequired: ["git.status.read"],
      capabilitiesOptional: ["ntm.panes.stream"], // missing
      degradedMode: {
        ifMissingRequired: "emit_diagnostic",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "activity_panel_warning",
      },
    });
    expect(dec.render).toBe("available");
    expect(dec.missingOptional).toEqual(["ntm.panes.stream"]);
  });

  test("all-required-ok and no optionals → available", () => {
    const dec = determineFeature(REGISTRY_FIXTURE, {
      featureId: "test.allgood",
      capabilitiesRequired: ["git.status.read", "agent_mail.messages.send"],
      capabilitiesOptional: [],
      degradedMode: {
        ifMissingRequired: "emit_diagnostic",
        ifMissingOptional: "continue_with_warning",
        activityBehavior: "silent",
      },
    });
    expect(dec.render).toBe("available");
    expect(dec.missingRequired).toEqual([]);
    expect(dec.degradedReasons).toEqual([]);
  });
});

describe("FEATURE_CATALOG", () => {
  test("each entry has matching featureId key + value", () => {
    for (const [key, requirement] of Object.entries(FEATURE_CATALOG)) {
      expect(requirement.featureId).toBe(key);
    }
  });

  test("every required cap reference parses to a known tool", () => {
    for (const requirement of Object.values(FEATURE_CATALOG)) {
      const allRefs = [
        ...requirement.capabilitiesRequired,
        ...requirement.capabilitiesOptional,
      ];
      for (const ref of allRefs) {
        const tool = toolFromCapRef(ref);
        if (!tool) {
          throw new Error(
            `feature ${requirement.featureId} references unknown tool in capability ${ref}`,
          );
        }
      }
    }
  });
});

describe("decideFeature", () => {
  test("resolves catalog entries via featureId", () => {
    const dec = decideFeature(REGISTRY_FIXTURE, "swarm.bead.push-branch");
    expect(dec.render).toBe("blocked-by-policy");
  });

  test("throws for unknown feature ids", () => {
    expect(() => decideFeature(REGISTRY_FIXTURE, "nope" as never)).toThrow();
  });
});

describe("renderBucketFor", () => {
  test("untested maps to unavailable for the renderer", () => {
    expect(renderBucketFor("untested")).toBe("unavailable");
  });

  test("missing maps to unavailable", () => {
    expect(renderBucketFor("missing")).toBe("unavailable");
  });

  test("ok → available", () => {
    expect(renderBucketFor("ok")).toBe("available");
  });

  test("degraded → degraded", () => {
    expect(renderBucketFor("degraded")).toBe("degraded");
  });

  test("blocked-by-policy → blocked-by-policy", () => {
    expect(renderBucketFor("blocked-by-policy")).toBe("blocked-by-policy");
  });
});

// Parser-success-is-not-capability-success — the load-bearing §2.8 contract.
// This pins it on the renderer: even if a tool report parses cleanly with
// every required capability marked blocked-by-policy, the feature must
// resolve to blocked-by-policy (not available).
test("§2.8: parser success does not imply feature availability", () => {
  const requirement: FeatureCapabilityRequirement = {
    featureId: "test.parser-success",
    capabilitiesRequired: ["git.push"],
    capabilitiesOptional: [],
    degradedMode: {
      ifMissingRequired: "block_job",
      ifMissingOptional: "continue_with_warning",
      activityBehavior: "activity_panel_warning",
    },
  };
  const dec = determineFeature(REGISTRY_FIXTURE, requirement);
  expect(dec.render).toBe("blocked-by-policy");
});
