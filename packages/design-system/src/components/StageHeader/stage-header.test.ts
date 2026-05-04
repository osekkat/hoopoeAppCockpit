import { expect, test } from "bun:test";
import {
  getStageHeaderModel,
  stageHeaderStages,
  type StageHeaderStageId,
} from "./stage-header.ts";

test("StageHeader maps every known stage to STAGE N -- VERB framing", () => {
  for (const stageId of Object.keys(stageHeaderStages) as StageHeaderStageId[]) {
    const model = getStageHeaderModel({
      stageId,
      projectName: "Hoopoe",
    });

    expect(model.stage.id).toBe(stageId);
    expect(model.kicker).toContain(`STAGE ${model.stage.number}`);
    expect(model.kicker).toContain(model.stage.verb);
    expect(model.title).toBe(model.stage.label);
  }
});

test("StageHeader includes the project name before supplied breadcrumbs", () => {
  const model = getStageHeaderModel({
    stageId: "bead",
    projectName: "Local demo",
    breadcrumb: ["Beads", { label: "hp-elx", href: "/beads/hp-elx" }],
  });

  expect(model.breadcrumbs).toEqual([
    { label: "Local demo", href: null },
    { label: "Beads", href: null },
    { label: "hp-elx", href: "/beads/hp-elx" },
  ]);
});

test("StageHeader normalizes actions with active and disabled state", () => {
  const model = getStageHeaderModel({
    stageId: "swarm",
    projectName: "Local demo",
    activeActionId: "launch",
    actions: [
      { id: "launch", label: "Launch swarm", tone: "primary" },
      { id: "halt", label: "Halt", tone: "danger", disabled: true },
    ],
  });

  expect(model.actions).toEqual([
    {
      id: "launch",
      label: "Launch swarm",
      ariaLabel: "Launch swarm",
      tone: "primary",
      disabled: false,
      active: true,
    },
    {
      id: "halt",
      label: "Halt",
      ariaLabel: "Halt",
      tone: "danger",
      disabled: true,
      active: false,
    },
  ]);
});

test("StageHeader supports title and subtitle overrides", () => {
  const model = getStageHeaderModel({
    stageId: "harden",
    projectName: "Local demo",
    title: "Review Round 5",
    subtitle: "UBS hotspot pass",
  });

  expect(model.title).toBe("Review Round 5");
  expect(model.subtitle).toBe("UBS hotspot pass");
  expect(model.ariaLabel).toContain("Debugging / Hardening");
});
