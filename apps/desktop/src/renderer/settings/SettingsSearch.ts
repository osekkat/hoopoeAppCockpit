// Hoopoe-owned. Settings ⌘F search (hp-wg5p).
//
// Pure function: takes the descriptor list + a query, returns the
// matching descriptors with a score. Token-based matching across
// label / description / keywords / section name. Empty query returns
// all descriptors with score 0 (no dimming).

import {
  SETTING_DESCRIPTORS,
  SECTION_LABELS,
  type SettingDescriptor,
} from "./SettingsModel.ts";

export interface SettingsSearchHit {
  readonly descriptor: SettingDescriptor;
  /** 0 = no match, higher = better. Empty query yields 0 for everything. */
  readonly score: number;
  /** Matched substrings (lowercased) — used by the renderer to highlight
   *  inline. */
  readonly matchedTerms: readonly string[];
}

/** Search the settings catalog. Empty/whitespace-only query returns the
 *  entire catalog with score=0. */
export function searchSettings(
  query: string,
  descriptors: readonly SettingDescriptor[] = SETTING_DESCRIPTORS,
): SettingsSearchHit[] {
  const tokens = query
    .toLowerCase()
    .split(/\s+/)
    .filter((t) => t.length > 0);
  if (tokens.length === 0) {
    return descriptors.map((d) => ({ descriptor: d, score: 0, matchedTerms: [] }));
  }
  const out: SettingsSearchHit[] = [];
  for (const descriptor of descriptors) {
    const haystacks: Array<{ text: string; weight: number }> = [
      { text: descriptor.label.toLowerCase(), weight: 4 },
      { text: descriptor.description.toLowerCase(), weight: 2 },
      { text: descriptor.key.toLowerCase(), weight: 3 },
      { text: SECTION_LABELS[descriptor.section].toLowerCase(), weight: 1 },
      ...((descriptor.keywords ?? []).map((kw) => ({ text: kw.toLowerCase(), weight: 3 }))),
    ];
    let score = 0;
    const matched = new Set<string>();
    for (const token of tokens) {
      let tokenHit = false;
      for (const { text, weight } of haystacks) {
        if (text.includes(token)) {
          score += weight;
          tokenHit = true;
          matched.add(token);
        }
      }
      // Penalize when a token doesn't match anywhere — pushes the
      // descriptor below "all tokens hit somewhere" results.
      if (!tokenHit) score -= 1;
    }
    if (score > 0) {
      out.push({ descriptor, score, matchedTerms: Array.from(matched) });
    }
  }
  out.sort((a, b) => b.score - a.score);
  return out;
}

/** Convenience for the renderer: returns the dimmed set (descriptors
 *  with NO match) given a non-empty query. Empty query returns []. */
export function dimmedDescriptors(
  query: string,
  descriptors: readonly SettingDescriptor[] = SETTING_DESCRIPTORS,
): SettingDescriptor[] {
  if (query.trim().length === 0) return [];
  const hits = new Set(searchSettings(query, descriptors).map((h) => h.descriptor.key));
  return descriptors.filter((d) => !hits.has(d.key));
}
