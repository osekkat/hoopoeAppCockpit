// Hoopoe-owned barrel re-export for the vendored t3code settings helpers.
// Adaptations and the actual three-tier resolver live in
// `apps/desktop/src/main/SettingsBridge.ts`.

export { writeFileStringAtomically } from "./atomicWrite.ts";
export { stripDefaults, structurallyEqual } from "./stripDefaults.ts";
export type { StripDefaultsOptions } from "./stripDefaults.ts";
