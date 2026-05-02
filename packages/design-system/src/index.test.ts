import { expect, test } from "bun:test";
import {
  COVERAGE_RAMP,
  HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME,
} from "./index.ts";

test("design-system scaffold exposes package identity + token shapes", () => {
  expect(HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME).toBe("@hoopoe/design-system");
  expect(COVERAGE_RAMP.length).toBe(3);
  expect(COVERAGE_RAMP[0]?.label).toBe("low");
  expect(COVERAGE_RAMP[2]?.label).toBe("high");
});
