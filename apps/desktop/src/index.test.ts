import { expect, test } from "bun:test";
import { HOOPOE_DESKTOP_PACKAGE_NAME, HOOPOE_DESKTOP_VERSION } from "./index.ts";

test("desktop scaffold exposes package identity", () => {
  expect(HOOPOE_DESKTOP_PACKAGE_NAME).toBe("@hoopoe/desktop");
  expect(HOOPOE_DESKTOP_VERSION).toBe("0.0.0");
});
