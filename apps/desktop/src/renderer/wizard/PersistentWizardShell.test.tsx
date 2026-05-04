// hp-zsp1 - settings-backed wizard persistence tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  PersistentWizardShell,
  WizardReplaySink,
  buildWizardSettingsPatch,
  wizardRunsFromSettings,
} from "./index.ts";

test("PersistentWizardShell: falls back to in-memory sink when no settings bridge exists", () => {
  const html = renderToStaticMarkup(<PersistentWizardShell settingsBridge={null} />);
  expect(html).toContain('data-testid="wizard"');
  expect(html).toContain('data-current-step="path"');
});

test("wizardRunsFromSettings: reads the persisted wizard state file shape", () => {
  const runs = wizardRunsFromSettings({
    wizard: {
      schemaVersion: 1,
      runs: [
        {
          runId: "run-1",
          startedAt: "2026-05-04T07:00:00Z",
          path: "existing_vps",
          checkpoints: [
            {
              stepId: "path",
              outcome: "completed",
              recordedAt: "2026-05-04T07:00:01Z",
              data: { path: "existing_vps" },
            },
          ],
        },
      ],
    },
  });
  expect(runs).toHaveLength(1);
  expect(runs[0]?.runId).toBe("run-1");
  expect(runs[0]?.checkpoints[0]?.stepId).toBe("path");
});

test("buildWizardSettingsPatch: writes one wizard key for hoopoe.settings.set", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "run-2", now: () => new Date("2026-05-04T07:00:00Z") });
  sink.recordActivePath("local_demo");
  sink.recordCheckpoint({
    stepId: "path",
    outcome: "completed",
    now: () => new Date("2026-05-04T07:00:01Z"),
  });
  const patch = buildWizardSettingsPatch(sink);
  expect(Object.keys(patch)).toEqual(["wizard"]);
  expect(patch.wizard.schemaVersion).toBe(1);
  expect(patch.wizard.runs[0]?.path).toBe("local_demo");
});
