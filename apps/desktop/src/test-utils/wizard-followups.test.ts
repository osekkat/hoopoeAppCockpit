// hp-sgzb — STEP_FOLLOWUPS must resolve through the br robot surface.

import { spawnSync } from "node:child_process";
import { test } from "bun:test";
import { STEP_FOLLOWUPS } from "../renderer/wizard/WizardShell.tsx";

test(
  "STEP_FOLLOWUPS: every non-null value resolves via `br show` when br is on PATH",
  () => {
    // CI gate per hp-sgzb DOD: every follow-up bead actually exists in the
    // beads graph. Skips when br isn't installed (developer machine without
    // the toolchain) so the test pack still runs against a fresh checkout.
    const probe = spawnSync("br", ["--version"], { stdio: "ignore" });
    if (probe.status !== 0) {
      return;
    }
    const seen = new Set<string>();
    for (const followup of Object.values(STEP_FOLLOWUPS)) {
      if (followup === null) continue;
      if (seen.has(followup)) continue;
      seen.add(followup);
      const result = spawnSync("br", ["show", followup, "--json"], {
        encoding: "utf8",
        env: { ...process.env, CI: "1" },
      });
      if (result.status !== 0) {
        throw new Error(
          `br show ${followup} failed (exit ${result.status}): ${result.stderr.trim()}`,
        );
      }
      let parsed: unknown;
      try {
        parsed = JSON.parse(result.stdout);
      } catch (err) {
        throw new Error(`br show ${followup} returned non-JSON: ${(err as Error).message}`);
      }
      if (!Array.isArray(parsed) || parsed.length === 0) {
        throw new Error(`br show ${followup} returned empty result — bead not in graph`);
      }
    }
  },
  30_000,
);
