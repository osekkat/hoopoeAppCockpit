#!/usr/bin/env bash
# scripts/snapshot/seed/build-golden-outputs.sh
#
# Hoopoe Phase 0 (hp-wle) — adapter-contract-test golden-outputs seeder.
#
# For each adapter slug, produces six fixture files matching the §18.3
# contract:
#   normal.json
#   missing-tool.json
#   unsupported-version.json
#   malformed-json.json
#   timeout.json
#   high-volume.json
#
# `normal.json` is sourced from a real snapshot.json capture when one
# exists for that adapter; otherwise it falls back to a stub.
# The five failure-class fixtures are synthesized to exercise specific
# adapter-contract-test paths.

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

SNAPSHOT=""
FIXTURES_VERSION="phase0-2026-05-02"
OUT_DIR="$REPO_ROOT/packages/fixtures/golden-outputs"
ADAPTERS=(br bv ntm agent_mail git ru health caam caut dcg casr ubs jsm jfp oracle pt srp sbh)

while [[ $# -gt 0 ]]; do
  case "$1" in
    --snapshot) SNAPSHOT="$2"; shift 2;;
    --fixtures-version) FIXTURES_VERSION="$2"; shift 2;;
    --out) OUT_DIR="$2"; shift 2;;
    --adapter) ADAPTERS=("$2"); shift 2;;
    -h|--help)
      sed -n '1,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | grep -E '^#( |$)' | sed 's/^# \{0,1\}//'
      exit 0;;
    *) echo "build-golden-outputs.sh: unknown flag: $1" >&2; exit 2;;
  esac
done

if [[ -z "$SNAPSHOT" ]]; then
  echo "build-golden-outputs.sh: --snapshot is required" >&2
  exit 2
fi

NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

write_normal() {
  local adapter="$1"; local label="$2"; local out="$3"
  local cap="$(jq -c --arg a "$adapter" --arg l "$label" '.captures[$a].captures[$l] // null' "$SNAPSHOT")"
  local source="snapshot.sh --self-test (local 2026-05-02)"
  local kind="realistic"
  if [[ "$cap" == "null" ]]; then
    kind="stub"
    source="hand-written stub (real-VPS pin pending hp-r7i)"
    cap="$(jq -cn --arg a "$adapter" --arg l "$label" '{argv: [$a, $l], exit: 0, durationMs: 50, stdoutBytes: 2, stdoutJson: {}, stdoutText: "{}", stderrBytes: 0, stderrText: "", truncated: false, redacted: false, tags: ["stub"]}')"
  fi
  jq -c \
    --arg adapter "$adapter" \
    --arg state "normal" \
    --arg kind "$kind" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    --arg src "$source" \
    --argjson caps "$(jq -c --arg a "$adapter" '.captures[$a].capabilities // {}' "$SNAPSHOT")" \
    '{meta: {adapter: $adapter, state: $state, kind: $kind, fixturesVersion: $fv, capturedAt: $now, source: $src}}
     + .
     + (if ($caps | length) > 0 then {capabilities: $caps} else {} end)' <<<"$cap" \
    | jq . > "$out"
}

write_missing_tool() {
  local adapter="$1"; local out="$2"
  jq -cn \
    --arg adapter "$adapter" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    '{meta: {adapter: $adapter, state: "missing-tool", kind: "synthetic", fixturesVersion: $fv, capturedAt: $now, source: "hand-written"},
      argv: [$adapter, "--version"],
      exit: 127,
      durationMs: 5,
      stdoutBytes: 0,
      stdoutText: "",
      stderrBytes: 38,
      stderrText: ($adapter + ": command not found\n"),
      truncated: false,
      redacted: false,
      tags: ["missing-tool"],
      capabilities: {(($adapter + "._present")): {status: "missing"}}}' \
    | jq . > "$out"
}

write_unsupported_version() {
  local adapter="$1"; local out="$2"
  jq -cn \
    --arg adapter "$adapter" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    '{meta: {adapter: $adapter, state: "unsupported-version", kind: "synthetic", fixturesVersion: $fv, capturedAt: $now, source: "hand-written; mimics --version probe of pre-min-compatible binary"},
      argv: [$adapter, "--version"],
      exit: 0,
      durationMs: 12,
      stdoutBytes: 14,
      stdoutText: ($adapter + " 0.0.1\n"),
      stderrBytes: 0,
      stderrText: "",
      truncated: false,
      redacted: false,
      tags: ["unsupported-version"],
      capabilities: {(($adapter + "._minVersion")): {status: "missing", notes: "observed 0.0.1; min-compatible per integration contract is higher"}}}' \
    | jq . > "$out"
}

write_malformed_json() {
  local adapter="$1"; local out="$2"
  jq -cn \
    --arg adapter "$adapter" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    '{meta: {adapter: $adapter, state: "malformed-json", kind: "synthetic", fixturesVersion: $fv, capturedAt: $now, source: "hand-written; tests parser graceful-degradation"},
      argv: [$adapter, "--json", "list"],
      exit: 0,
      durationMs: 35,
      stdoutBytes: 38,
      stdoutText: "{\"truncated_at_first_object\": tru",
      stderrBytes: 0,
      stderrText: "",
      truncated: false,
      redacted: false,
      tags: ["malformed-json"],
      capabilities: {(($adapter + "._parse")): {status: "degraded", notes: "stdout was non-JSON; adapter must NOT panic"}}}' \
    | jq . > "$out"
}

write_timeout() {
  local adapter="$1"; local out="$2"
  jq -cn \
    --arg adapter "$adapter" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    '{meta: {adapter: $adapter, state: "timeout", kind: "synthetic", fixturesVersion: $fv, capturedAt: $now, source: "hand-written; mimics ENVELOPE_TIMEOUT_S exhaustion"},
      argv: [$adapter, "--json", "list"],
      exit: 124,
      durationMs: 30000,
      stdoutBytes: 0,
      stdoutText: "",
      stderrBytes: 27,
      stderrText: "timeout: sending signal TERM",
      truncated: false,
      redacted: false,
      tags: ["timeout"],
      capabilities: {(($adapter + "._timeout")): {status: "degraded", notes: "exceeded ENVELOPE_TIMEOUT_S; adapter must surface; do not retry without backoff"}}}' \
    | jq . > "$out"
}

write_high_volume() {
  local adapter="$1"; local out="$2"
  jq -cn \
    --arg adapter "$adapter" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    --arg cap "1048576" \
    '{meta: {adapter: $adapter, state: "high-volume", kind: "synthetic", fixturesVersion: $fv, capturedAt: $now, source: "hand-written; mimics ENVELOPE_MAX_BYTES truncation"},
      argv: [$adapter, "--json", "dump-everything"],
      exit: 0,
      durationMs: 4200,
      stdoutBytes: ($cap | tonumber),
      stdoutText: "<truncated 1MiB worth of JSON>",
      stderrBytes: 0,
      stderrText: "",
      truncated: true,
      redacted: false,
      tags: ["high-volume", "truncated"],
      capabilities: {(($adapter + "._highVolume")): {status: "degraded", notes: "stdout exceeded ENVELOPE_MAX_BYTES; pagination required (per integration contract)"}}}' \
    | jq . > "$out"
}

# Map of preferred normal-state captures from snapshot.sh per adapter.
preferred_normal_label() {
  case "$1" in
    git) echo "status_porcelain";;
    br) echo "list_open";;
    bv) echo "robot_triage";;
    ntm) echo "robot_snapshot";;
    agent_mail) echo "help";;
    ru) echo "schema";;
    health) echo "lizard_version";;
    caam) echo "accounts_list";;
    caut) echo "usage_json";;
    dcg) echo "status";;
    casr) echo "status";;
    ubs) echo "help";;
    jsm) echo "list";;
    jfp) echo "list";;
    oracle) echo "help";;
    pt) echo "help";;
    srp) echo "signals";;
    sbh) echo "help";;
    *) echo "help";;
  esac
}

for adapter in "${ADAPTERS[@]}"; do
  dir="$OUT_DIR/$adapter"
  mkdir -p "$dir"
  label="$(preferred_normal_label "$adapter")"
  write_normal "$adapter" "$label" "$dir/normal.json"
  write_missing_tool "$adapter" "$dir/missing-tool.json"
  write_unsupported_version "$adapter" "$dir/unsupported-version.json"
  write_malformed_json "$adapter" "$dir/malformed-json.json"
  write_timeout "$adapter" "$dir/timeout.json"
  write_high_volume "$adapter" "$dir/high-volume.json"
  echo "build-golden-outputs.sh: seeded $adapter (normal=$label) → $dir/" >&2
done

# README for golden-outputs/.
cat > "$OUT_DIR/README.md" <<'README'
# `golden-outputs/` — adapter contract test corpus

One file per `(adapter, state)` pair. `state` is one of: `normal`, `missing-tool`, `unsupported-version`, `malformed-json`, `timeout`, `high-volume` (per `plan.md` §18.3).

Adapter contract tests load `golden-outputs/<adapter>/<state>.json` and assert:

- The adapter parses `stdoutJson` / `stdoutText` correctly.
- The adapter reports the same capability state as the fixture's `capabilities` block.
- For `state: malformed-json`, the parser fails *gracefully* (no panic; logs the error; reports `degraded`).
- For `state: missing-tool`, the adapter reports `present: false` and the right `skipReason`.
- For `state: timeout`, the adapter surfaces the timeout without retrying without backoff.
- For `state: high-volume`, the adapter respects pagination contracts (`--limit` / `--offset`) and never raises `--max-bytes` past the schema cap.

`normal.json` files are realistic when the source snapshot has a capture for them; otherwise they are stubs marked `meta.kind: stub`. The five failure-class fixtures are always `synthetic` (real systems don't exhibit them on demand).

Regenerate via:

```bash
scripts/snapshot/seed/build-golden-outputs.sh \
  --snapshot /tmp/hoopoe-snapshot-full.json \
  --fixtures-version phase0-2026-05-02
```
README

echo "build-golden-outputs.sh: done; out=$OUT_DIR" >&2
