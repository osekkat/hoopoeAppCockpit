import { expect, test } from "bun:test";
import { HOOPOE_OPENAPI_VERSION, HOOPOE_SCHEMAS_PACKAGE_NAME } from "./index.ts";

test("schemas scaffold exposes package identity", () => {
  expect(HOOPOE_SCHEMAS_PACKAGE_NAME).toBe("@hoopoe/schemas");
  expect(HOOPOE_OPENAPI_VERSION).toBe("0.0.0-pre");
});
