// `@hoopoe/test-evidence` — `@slo:<targetId>` tag parser (hp-6sv).
//
// Tests opt into SLO assertion by including `@slo:<targetId>` in their
// describe / it / test name. The targetId must match an entry in
// `packages/slo-targets.yaml`. The parser also strips other category
// tags (`@unit`, `@e2e`, `@chaos`, `@release`, `@smoke`) so the
// evidence file's `result.name` is the human-readable name without
// tag noise.

export const TAG_REGEX = /@([a-z][a-z0-9_-]*)(?::([A-Za-z0-9_.\-]+))?/g;

export const KNOWN_CATEGORY_TAGS: ReadonlySet<string> = new Set([
  "unit",
  "integration",
  "e2e",
  "chaos",
  "smoke",
  "release",
  "slo",
]);

export interface ParsedTags {
  /** Test name with all `@<tag>` substrings removed and double-spaces collapsed. */
  cleanName: string;
  /** Category tags found (e.g., `e2e`, `slo`). */
  categories: readonly string[];
  /** SLO target id if `@slo:<targetId>` was present. */
  sloTarget: string | null;
  /** Other tag values, keyed by tag name. */
  other: Readonly<Record<string, readonly string[]>>;
}

export function parseTags(name: string): ParsedTags {
  const categories = new Set<string>();
  const other: Record<string, string[]> = {};
  let sloTarget: string | null = null;
  let working = name;
  TAG_REGEX.lastIndex = 0;
  let match: RegExpExecArray | null = TAG_REGEX.exec(name);
  while (match !== null) {
    const [, rawKey, rawValue] = match;
    const key = (rawKey ?? "").toLowerCase();
    if (KNOWN_CATEGORY_TAGS.has(key)) categories.add(key);
    if (key === "slo" && rawValue !== undefined) sloTarget = rawValue;
    else if (rawValue !== undefined) {
      const slot = other[key] ?? [];
      slot.push(rawValue);
      other[key] = slot;
    }
    match = TAG_REGEX.exec(name);
  }
  // Strip every match in a second pass (after enumeration to avoid lastIndex
  // surprises).
  working = name.replace(TAG_REGEX, "").replace(/\s{2,}/g, " ").trim();
  // Strip leading/trailing parentheses + dashes left behind by tag removal.
  working = working.replace(/^[\s\-:|]+|[\s\-:|]+$/g, "");
  return {
    cleanName: working,
    categories: Array.from(categories).sort(),
    sloTarget,
    other,
  };
}
