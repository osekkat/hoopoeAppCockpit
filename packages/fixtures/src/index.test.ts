import { expect, test } from "bun:test";
import { HOOPOE_FIXTURES_PACKAGE_NAME } from "./index.ts";

test("fixtures scaffold exposes package identity", () => {
  expect(HOOPOE_FIXTURES_PACKAGE_NAME).toBe("@hoopoe/fixtures");
});
