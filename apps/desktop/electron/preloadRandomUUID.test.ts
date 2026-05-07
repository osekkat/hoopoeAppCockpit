// hp-r1tk: lock the contract that the preload's subscriptionId
// factory returns RFC 4122 v4 UUIDs. The regex matches the version
// nibble (4) and the variant nibble (8/9/a/b) per the spec.

import { expect, test } from "bun:test";
import { randomUUID } from "./preloadRandomUUID.ts";

const UUID_V4_REGEX =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

test("randomUUID: returns an RFC 4122 v4 UUID (lowercase hex, version nibble 4, variant nibble 8/9/a/b)", () => {
  const got = randomUUID();
  expect(got).toMatch(UUID_V4_REGEX);
});

test("randomUUID: distinct values across consecutive calls (collision-resistance smoke check)", () => {
  const seen = new Set<string>();
  for (let i = 0; i < 100; i += 1) {
    const id = randomUUID();
    expect(id).toMatch(UUID_V4_REGEX);
    expect(seen.has(id)).toBe(false);
    seen.add(id);
  }
  expect(seen.size).toBe(100);
});

test("randomUUID: returned string has the canonical 36-character length (8-4-4-4-12 + 4 hyphens)", () => {
  expect(randomUUID()).toHaveLength(36);
});
