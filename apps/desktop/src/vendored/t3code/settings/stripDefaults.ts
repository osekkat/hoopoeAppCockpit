// Originally from github.com/pingdotgg/t3code (MIT License)
// Copyright (c) 2026 T3 Tools Inc.
// Adapted for Hoopoe.
//
// Full MIT license text: vendored/t3code/LICENSE
//
// Lifted from t3code `apps/server/src/serverSettings.ts` lines 207–239:
// the `stripDefaultServerSettings` recursive helper. The Effect-`Equal`
// dependency is replaced with a plain structural equality helper so the
// vendored module is self-contained.
//
// Forward-compat rationale: when settings round-trip through this helper
// before being written, the on-disk file contains only keys whose value
// differs from the in-memory defaults. A future bump of a default value
// then propagates to every existing user without an explicit migration —
// the on-disk file stays silent on that key, and the in-process default
// fills it in on read.

/** Keys whose value should be compared atomically (as a whole) instead of
 * recursing into their structure. The caller passes this set; the vendored
 * code does not embed Hoopoe-specific names. */
export interface StripDefaultsOptions {
  readonly atomicKeys?: ReadonlySet<string>;
}

export function stripDefaults(
  current: unknown,
  defaults: unknown,
  options: StripDefaultsOptions = {},
): unknown {
  const atomicKeys = options.atomicKeys ?? new Set<string>();

  if (Array.isArray(current) || Array.isArray(defaults)) {
    return structurallyEqual(current, defaults) ? undefined : current;
  }

  if (
    current !== null &&
    defaults !== null &&
    typeof current === "object" &&
    typeof defaults === "object"
  ) {
    const currentRecord = current as Record<string, unknown>;
    const defaultsRecord = defaults as Record<string, unknown>;
    const next: Record<string, unknown> = {};

    for (const key of Object.keys(currentRecord)) {
      if (atomicKeys.has(key)) {
        if (!structurallyEqual(currentRecord[key], defaultsRecord[key])) {
          next[key] = currentRecord[key];
        }
      } else {
        const stripped = stripDefaults(
          currentRecord[key],
          defaultsRecord[key],
          options,
        );
        if (stripped !== undefined) {
          next[key] = stripped;
        }
      }
    }

    return Object.keys(next).length > 0 ? next : undefined;
  }

  return Object.is(current, defaults) ? undefined : current;
}

/** Replacement for Effect's `Equal.equals`: structural deep-equal. */
export function structurallyEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (a === null || b === null) return false;
  if (typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    for (let index = 0; index < a.length; index += 1) {
      if (!structurallyEqual(a[index], b[index])) return false;
    }
    return true;
  }
  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);
  if (aKeys.length !== bKeys.length) return false;
  for (const key of aKeys) {
    if (!Object.prototype.hasOwnProperty.call(b, key)) return false;
    if (
      !structurallyEqual(
        (a as Record<string, unknown>)[key],
        (b as Record<string, unknown>)[key],
      )
    ) {
      return false;
    }
  }
  return true;
}
