#!/usr/bin/env bash
# scripts/snapshot/seed/build-scenarios.sh
#
# Hoopoe Phase 0 (hp-wle) — fixture-corpus seeder.
#
# Reads a snapshot.json produced by scripts/research-spike/snapshot.sh
# and seeds the §8.8 tending scenarios under
# packages/fixtures/scenarios/<id>/ with realistic + synthesized data.
#
# Usage:
#   scripts/snapshot/seed/build-scenarios.sh \
#     --snapshot /tmp/hoopoe-snapshot-full.json \
#     --fixtures-version phase0-2026-05-02 \
#     [--scenario healthy-hour|idle-but-not-stuck|wedged-pane|rate-limited-no-caam|...|all] \
#     [--out packages/fixtures/scenarios]
#
# What it does, per scenario:
#   - Extracts bv-triage / br-list / ntm-snapshot / agent-mail-dump /
#     reservations from the snapshot when present.
#   - Synthesizes events.jsonl, pane-log placeholders, build-log
#     placeholders, capabilities.json, and expected-outcome.json
#     according to the §8.8 spec for that scenario.
#   - Writes a meta.json marking each fixture as realistic or synthetic.
#
# Idempotent: re-running overwrites the scenario's files (use git to
# review). Never deletes other scenarios' files.

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

SNAPSHOT=""
FIXTURES_VERSION="phase0-2026-05-02"
SCENARIO="all"
OUT_DIR="$REPO_ROOT/packages/fixtures/scenarios"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --snapshot) SNAPSHOT="$2"; shift 2;;
    --fixtures-version) FIXTURES_VERSION="$2"; shift 2;;
    --scenario) SCENARIO="$2"; shift 2;;
    --out) OUT_DIR="$2"; shift 2;;
    -h|--help)
      sed -n '1,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | grep -E '^#( |$)' | sed 's/^# \{0,1\}//'
      exit 0;;
    *) echo "build-scenarios.sh: unknown flag: $1" >&2; exit 2;;
  esac
done

if [[ -z "$SNAPSHOT" ]]; then
  echo "build-scenarios.sh: --snapshot is required" >&2
  exit 2
fi
if [[ ! -f "$SNAPSHOT" ]]; then
  echo "build-scenarios.sh: snapshot not found: $SNAPSHOT" >&2
  exit 2
fi

NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ALL_SCENARIOS=(healthy-hour idle-but-not-stuck wedged-pane rate-limited-no-caam rate-limited-with-caam stale-reservation commit-burst budget-breach skill-drift missing-tool postcondition-failure action-arbitration)

scenarios_to_build() {
  if [[ "$SCENARIO" == "all" ]]; then
    printf '%s\n' "${ALL_SCENARIOS[@]}"
  else
    printf '%s\n' "$SCENARIO"
  fi
}

# ---- helpers ----

extract_or_stub() {
  # extract_or_stub <jq-path> <fallback-json-literal>
  local path="$1"; local fallback="$2"
  jq -c "$path // null" "$SNAPSHOT" \
    | jq -c --argjson fb "$fallback" 'if . == null then $fb else . end'
}

write_meta() {
  local dir="$1"; local kind="$2"; local notes="$3"
  jq -cn \
    --arg kind "$kind" \
    --arg sc "$(basename "$dir")" \
    --arg fv "$FIXTURES_VERSION" \
    --arg now "$NOW" \
    --arg vps "$(jq -r '.meta.vpsId // "mock-flywheel"' "$SNAPSHOT")" \
    --arg src "snapshot.sh + scripts/snapshot/seed/build-scenarios.sh" \
    --arg notes "$notes" \
    '{kind: $kind, scenario: $sc, fixturesVersion: $fv, capturedAt: $now, vpsId: $vps, source: $src, notes: $notes}' \
    | jq . > "$dir/meta.json"
}

write_capabilities() {
  # Use the snapshot's per-tool capability declarations as the baseline.
  local dir="$1"; local mutations="$2"
  jq -c '.captures | to_entries
         | map({key: .key, value: (.value.capabilities // {}) })
         | from_entries' "$SNAPSHOT" \
    | jq --argjson mut "$mutations" '. * $mut' \
    | jq . > "$dir/capabilities.json"
}

write_jsonl_events() {
  # Newline-delimited JSON. Each line is a WS-event envelope.
  local dir="$1"; shift
  local out="$dir/events.jsonl"
  : > "$out"
  for ev in "$@"; do
    printf '%s\n' "$ev" >> "$out"
  done
}

write_text() {
  local path="$1"; local content="$2"
  printf '%s\n' "$content" > "$path"
}

write_pane_log() {
  # Write a small byte-addressable PTY capture (not a real one — synthetic).
  local dir="$1"; local agent="$2"; local content="$3"
  printf '%s' "$content" > "$dir/pane-logs/${agent}.bin"
}

# ---- baseline extracts (used by every scenario) ----

BV_TRIAGE_JSON="$(jq -c '.captures.bv.captures.robot_triage.stdoutJson // .captures.bv.captures.robot_triage // null' "$SNAPSHOT")"
if [[ "$BV_TRIAGE_JSON" == "null" ]]; then
  BV_TRIAGE_JSON='{"_stub": true, "note": "bv --robot-triage output (pin on real VPS)", "bottlenecks": [], "recommendations": null, "summary": {}}'
fi

BR_LIST_JSON="$(jq -c '.captures.br.captures.list_open.stdoutJson // null' "$SNAPSHOT")"
if [[ "$BR_LIST_JSON" == "null" ]]; then
  BR_LIST_JSON='{"_stub": true, "issues": [], "total": 0, "has_more": false, "limit": 250, "offset": 0}'
fi

# Slim BR list for fixtures: keep only the first 8 issues so fixtures are small.
BR_LIST_SLIM_JSON="$(jq -c --argjson v "$BR_LIST_JSON" '$v | if has("issues") then .issues = (.issues[0:8]) | .total = ($v.issues | length) else . end' <<<"$BR_LIST_JSON" 2>/dev/null || echo "$BR_LIST_JSON")"

NTM_SNAPSHOT_JSON="$(jq -c '.captures.ntm.captures.robot_snapshot.stdoutJson // null' "$SNAPSHOT")"
if [[ "$NTM_SNAPSHOT_JSON" == "null" ]]; then
  NTM_SNAPSHOT_JSON='{"_stub": true, "sessions": [], "panes": []}'
fi

# ---- scenario bodies ----

build_healthy_hour() {
  local dir="$OUT_DIR/healthy-hour"
  mkdir -p "$dir/pane-logs" "$dir/build-logs"
  write_meta "$dir" "synthetic" "Real bv/br extracts from local self-test (2026-05-02). Pane state, mail, build logs synthesized for §8.8 healthy-hour."

  printf '%s' "$BV_TRIAGE_JSON" | jq . > "$dir/bv-triage.json"
  printf '%s' "$BR_LIST_SLIM_JSON" | jq . > "$dir/br-list.json"

  jq -cn \
    '{sessions: [{id: "hoopoe-implementation", windows: 1, panes: [
      {id: "%1", agent: "GreenBear", program: "claude-code", model: "claude-opus-4-7", state: "IDLE", last_activity_ts: "2026-05-02T19:55:00Z", bead: "hp-xru"},
      {id: "%2", agent: "BlueHill",   program: "codex-cli",   model: "GPT-5",         state: "TYPING", last_activity_ts: "2026-05-02T19:59:50Z", bead: "hp-k6r"},
      {id: "%3", agent: "FuchsiaPond",program: "codex-cli",   model: "gpt-5.5",       state: "IDLE", last_activity_ts: "2026-05-02T19:55:30Z", bead: "hp-ara"},
      {id: "%4", agent: "FuchsiaStone", program: "claude-code", model: "claude-opus-4-7", state: "TOOL_USE", last_activity_ts: "2026-05-02T19:59:55Z", bead: "hp-wle"}
    ]}], counters: {sessions: 1, panes: 4, alive: 4, wedged: 0}}' | jq . > "$dir/ntm-snapshot.json"

  jq -cn \
    --arg now "$NOW" \
    '{messages: [
      {id: 64, thread_id: "hoopoe-intro", from: "FuchsiaPond", to: ["all"], subject: "[hoopoe-intro] cross-domain pull: hp-ara", created_ts: "2026-05-02T19:52:16Z", importance: "normal", ack_required: false},
      {id: 67, thread_id: "hp-6v3", from: "FuchsiaStone", to: ["all"], subject: "[hp-6v3] Completed", created_ts: "2026-05-02T19:54:43Z", importance: "normal", ack_required: false}
     ],
     threads: ["hoopoe-intro", "hp-6v3", "hp-78m", "hp-k6r", "hp-ara"],
     unread_total: 0
    }' | jq . > "$dir/agent-mail-dump.json"

  jq -cn '{reservations: [
    {id: 47, agent: "GreenBear", paths: ["apps/desktop/src/main/**"], exclusive: true, reason: "hp-zir", expires_ts: "2026-05-02T22:00:00Z"},
    {id: 88, agent: "FuchsiaStone", paths: ["packages/fixtures/scenarios/**"], exclusive: true, reason: "hp-wle", expires_ts: "2026-05-02T22:11:52Z"}
  ], conflicts: []}' | jq . > "$dir/reservations.json"

  write_jsonl_events "$dir" \
    '{"channel":"swarm","seq":1,"ts":"2026-05-02T19:50:00Z","type":"swarm.tick","payload":{"alive":4,"wedged":0,"queued_beads":12}}' \
    '{"channel":"agent_state","seq":2,"ts":"2026-05-02T19:51:30Z","type":"agent.state_changed","payload":{"agent":"BlueHill","from":"IDLE","to":"TYPING","bead":"hp-k6r"}}' \
    '{"channel":"beads","seq":3,"ts":"2026-05-02T19:52:00Z","type":"bead.status_changed","payload":{"id":"hp-ara","from":"open","to":"in_progress","actor":"FuchsiaPond"}}' \
    '{"channel":"audit","seq":4,"ts":"2026-05-02T19:54:43Z","type":"bead.closed","payload":{"id":"hp-6v3","actor":"FuchsiaStone","reason":"snapshot script ready"}}'

  write_pane_log "$dir" "GreenBear"   "[2026-05-02 19:55:00] idle"
  write_pane_log "$dir" "BlueHill"    "[2026-05-02 19:59:50] typing tokens.css..."
  write_pane_log "$dir" "FuchsiaPond" "[2026-05-02 19:55:30] idle"
  write_pane_log "$dir" "FuchsiaStone" "[2026-05-02 19:59:55] tool_use: writing fixtures"

  write_text "$dir/build-logs/run-001.txt" "PASS apps/desktop/src/main/BackendLifecycle.test.ts (12 tests)
PASS packages/fixtures/src/index.test.ts (1 test)
Total: 13 passed"

  write_capabilities "$dir" '{}'

  jq -cn \
    '{meta: {kind: "synthetic", scenario: "healthy-hour", fixturesVersion: "'"$FIXTURES_VERSION"'", capturedAt: "'"$NOW"'", source: "build-scenarios.sh", notes: "Healthy hour: 4 active panes, no wedging, no rate-limit, mail quiet, no stale reservations."},
     detections: [],
     wakeAgent: false,
     approvalsRequested: [],
     postconditions: [{check: "swarm.alive == 4", expect: true}, {check: "no.wedged", expect: true}],
     activityBehavior: "silent"}' | jq . > "$dir/expected-outcome.json"
}

build_idle_but_not_stuck() {
  local dir="$OUT_DIR/idle-but-not-stuck"
  mkdir -p "$dir/pane-logs" "$dir/build-logs"
  write_meta "$dir" "synthetic" "All panes IDLE but recent enough that pre-script must NOT classify as wedged. Tests the IDLE vs WEDGED disambiguation in tend-swarm."

  printf '%s' "$BV_TRIAGE_JSON" | jq . > "$dir/bv-triage.json"
  printf '%s' "$BR_LIST_SLIM_JSON" | jq . > "$dir/br-list.json"

  jq -cn '{sessions: [{id: "hoopoe-implementation", windows: 1, panes: [
    {id: "%1", agent: "GreenBear", program: "claude-code", model: "claude-opus-4-7", state: "IDLE", last_activity_ts: "2026-05-02T19:58:00Z", bead: "hp-xru", idle_seconds: 90},
    {id: "%2", agent: "BlueHill",   program: "codex-cli",   model: "GPT-5",         state: "IDLE", last_activity_ts: "2026-05-02T19:58:00Z", bead: "hp-k6r", idle_seconds: 90},
    {id: "%3", agent: "FuchsiaPond",program: "codex-cli",   model: "gpt-5.5",       state: "IDLE", last_activity_ts: "2026-05-02T19:58:30Z", bead: "hp-ara", idle_seconds: 60},
    {id: "%4", agent: "FuchsiaStone",program: "claude-code", model: "claude-opus-4-7", state: "IDLE", last_activity_ts: "2026-05-02T19:58:30Z", bead: "hp-wle", idle_seconds: 60}
  ]}], counters: {sessions: 1, panes: 4, alive: 4, wedged: 0, idle_under_threshold: 4}}' | jq . > "$dir/ntm-snapshot.json"

  jq -cn '{messages: [], threads: [], unread_total: 0}' | jq . > "$dir/agent-mail-dump.json"
  jq -cn '{reservations: [], conflicts: []}' | jq . > "$dir/reservations.json"

  write_jsonl_events "$dir" \
    '{"channel":"swarm","seq":1,"ts":"2026-05-02T19:59:00Z","type":"swarm.tick","payload":{"alive":4,"wedged":0,"all_idle":true,"max_idle_s":90,"wedge_threshold_s":600}}'

  write_pane_log "$dir" "GreenBear"    "[2026-05-02 19:58:00] last response complete"
  write_pane_log "$dir" "BlueHill"     "[2026-05-02 19:58:00] last response complete"
  write_pane_log "$dir" "FuchsiaPond"  "[2026-05-02 19:58:30] last response complete"
  write_pane_log "$dir" "FuchsiaStone" "[2026-05-02 19:58:30] last response complete"

  write_text "$dir/build-logs/run-002.txt" "(no builds in window)"

  write_capabilities "$dir" '{}'

  jq -cn \
    '{meta: {kind: "synthetic", scenario: "idle-but-not-stuck", fixturesVersion: "'"$FIXTURES_VERSION"'", capturedAt: "'"$NOW"'", source: "build-scenarios.sh", notes: "All panes IDLE 60-90s; well under wedge_threshold_s=600. tend-swarm pre-script must report wakeAgent=false."},
     detections: [{kind: "swarm.idle", payload: {max_idle_s: 90, wedge_threshold_s: 600, classification: "idle"}}],
     wakeAgent: false,
     approvalsRequested: [],
     postconditions: [{check: "no.action_taken", expect: true}, {check: "no.audit_entry_for_intervention", expect: true}],
     activityBehavior: "silent"}' | jq . > "$dir/expected-outcome.json"
}

build_wedged_pane() {
  local dir="$OUT_DIR/wedged-pane"
  mkdir -p "$dir/pane-logs" "$dir/build-logs"
  write_meta "$dir" "synthetic" "One pane wedged (idle > wedge_threshold_s + last_output ends mid-line). tend-swarm should propose ActionPlan: agent.kill_wedged_process or agent.ask_status (per skill)."

  printf '%s' "$BV_TRIAGE_JSON" | jq . > "$dir/bv-triage.json"
  printf '%s' "$BR_LIST_SLIM_JSON" | jq . > "$dir/br-list.json"

  jq -cn '{sessions: [{id: "hoopoe-implementation", windows: 1, panes: [
    {id: "%1", agent: "GreenBear", program: "claude-code", model: "claude-opus-4-7", state: "TOOL_USE", last_activity_ts: "2026-05-02T19:42:11Z", bead: "hp-zir", idle_seconds: 1080, wedge_classification: "wedged", evidence: "tool_use mid-call; no progress 18 min; partial line in scrollback"},
    {id: "%2", agent: "BlueHill",   program: "codex-cli",   model: "GPT-5",         state: "IDLE",     last_activity_ts: "2026-05-02T19:58:00Z", bead: "hp-k6r", idle_seconds: 90},
    {id: "%3", agent: "FuchsiaPond",program: "codex-cli",   model: "gpt-5.5",       state: "TYPING",   last_activity_ts: "2026-05-02T19:59:55Z", bead: "hp-ara"}
  ]}], counters: {sessions: 1, panes: 3, alive: 3, wedged: 1}}' | jq . > "$dir/ntm-snapshot.json"

  jq -cn '{messages: [], threads: [], unread_total: 0}' | jq . > "$dir/agent-mail-dump.json"
  jq -cn '{reservations: [
    {id: 99, agent: "GreenBear", paths: ["apps/desktop/src/main.ts"], exclusive: true, reason: "hp-zir", expires_ts: "2026-05-02T20:42:11Z", note: "still held by wedged agent"}
  ], conflicts: []}' | jq . > "$dir/reservations.json"

  write_jsonl_events "$dir" \
    '{"channel":"swarm","seq":1,"ts":"2026-05-02T19:42:11Z","type":"agent.state_changed","payload":{"agent":"GreenBear","from":"TYPING","to":"TOOL_USE"}}' \
    '{"channel":"swarm","seq":2,"ts":"2026-05-02T19:50:00Z","type":"swarm.tick","payload":{"alive":3,"wedged":0,"warnings":["GreenBear no progress 8m"]}}' \
    '{"channel":"swarm","seq":3,"ts":"2026-05-02T19:55:00Z","type":"swarm.tick","payload":{"alive":3,"wedged":0,"warnings":["GreenBear no progress 13m"]}}' \
    '{"channel":"swarm","seq":4,"ts":"2026-05-02T20:00:11Z","type":"agent.classified_wedged","payload":{"agent":"GreenBear","idle_s":1080,"wedge_threshold_s":600,"evidence":"partial line; tool_use stuck"}}'

  write_pane_log "$dir" "GreenBear" "[2026-05-02 19:42:11] running tool: read_file('apps/desktop/src/main.ts')...
[2026-05-02 19:42:13] returned (12 KB)
[2026-05-02 19:42:14] applying patch... line 184: "
  write_pane_log "$dir" "BlueHill" "[2026-05-02 19:58:00] last response complete"
  write_pane_log "$dir" "FuchsiaPond" "[2026-05-02 19:59:55] typing..."

  write_text "$dir/build-logs/run-003.txt" "(no builds; wedged agent owns build target)"

  write_capabilities "$dir" '{"ntm":{"ntm.pane.kill":{"status":"ok","notes":"available; ActionPlan-gated"}},"agent_mail":{"agent_mail.reservations.force_release":{"status":"ok","notes":"available; ActionPlan-gated; approval required"}}}'

  jq -cn \
    '{meta: {kind: "synthetic", scenario: "wedged-pane", fixturesVersion: "'"$FIXTURES_VERSION"'", capturedAt: "'"$NOW"'", source: "build-scenarios.sh", notes: "Wedged agent: idle 18m, tool_use mid-call, holds reservation. Pre-script detects wedge; agent woken; ActionPlan: agent.ask_status first, then agent.kill_wedged_process if no response, then reservation.force_release."},
     detections: [
       {kind: "agent.wedged", payload: {agent: "GreenBear", idle_s: 1080, wedge_threshold_s: 600}},
       {kind: "reservation.held_by_wedged", payload: {agent: "GreenBear", reservation_id: 99}}
     ],
     wakeAgent: true,
     actionPlan: {actions: [
       {type: "agent.ask_status", args: {agent: "GreenBear", timeout_s: 60}},
       {type: "agent.kill_wedged_process", args: {agent: "GreenBear", reason: "no response after ask_status"}},
       {type: "reservation.force_release", args: {reservation_id: 99, reason: "owner agent killed"}}
     ]},
     approvalsRequested: [
       {scope: "agent.kill_wedged_process", reason: "killing GreenBear (P0; approve to proceed)"},
       {scope: "reservation.force_release", reason: "force-releasing apps/desktop/src/main.ts (held by killed agent)"}
     ],
     postconditions: [{check: "GreenBear.state == DEAD", expect: true}, {check: "reservation_99.released", expect: true}],
     activityBehavior: "surface"}' | jq . > "$dir/expected-outcome.json"
}

build_rate_limited() {
  # rate-limited-no-caam variant
  local dir="$OUT_DIR/rate-limited-no-caam"
  mkdir -p "$dir/pane-logs" "$dir/build-logs"
  write_meta "$dir" "synthetic" "Anthropic rate limit hit; CAAM has no alternative account configured. tend-swarm should propose pause + surface to user."

  printf '%s' "$BV_TRIAGE_JSON" | jq . > "$dir/bv-triage.json"
  printf '%s' "$BR_LIST_SLIM_JSON" | jq . > "$dir/br-list.json"

  jq -cn '{sessions: [{id: "hoopoe-implementation", windows: 1, panes: [
    {id: "%1", agent: "GreenBear", program: "claude-code", model: "claude-opus-4-7", state: "ERROR", last_activity_ts: "2026-05-02T19:58:00Z", bead: "hp-zir", error: "rate_limited", retry_after_s: 1800}
  ]}], counters: {sessions: 1, panes: 1, alive: 1, wedged: 0, errored: 1}}' | jq . > "$dir/ntm-snapshot.json"

  jq -cn '{messages: [], threads: [], unread_total: 0}' | jq . > "$dir/agent-mail-dump.json"
  jq -cn '{reservations: [], conflicts: []}' | jq . > "$dir/reservations.json"

  write_jsonl_events "$dir" \
    '{"channel":"agent_state","seq":1,"ts":"2026-05-02T19:58:00Z","type":"agent.errored","payload":{"agent":"GreenBear","kind":"rate_limited","retry_after_s":1800}}' \
    '{"channel":"caut","seq":2,"ts":"2026-05-02T19:58:01Z","type":"caut.usage_changed","payload":{"provider":"claude","percent":100,"reset_at":"2026-05-02T20:28:00Z"}}' \
    '{"channel":"caam","seq":3,"ts":"2026-05-02T19:58:02Z","type":"caam.account_status","payload":{"available_accounts":1,"alternatives":0}}'

  write_pane_log "$dir" "GreenBear" "[2026-05-02 19:58:00] error: rate_limit_exceeded; retry_after=1800s"
  write_text "$dir/build-logs/run-004.txt" "(no builds; agent rate-limited)"

  write_capabilities "$dir" '{"caam":{"caam.account.switch":{"status":"degraded","notes":"only one account configured; no alternative to switch to"}}}'

  jq -cn \
    '{meta: {kind: "synthetic", scenario: "rate-limited-no-caam", fixturesVersion: "'"$FIXTURES_VERSION"'", capturedAt: "'"$NOW"'", source: "build-scenarios.sh", notes: "Provider rate limit, no CAAM alternative. Pre-script detects; ActionPlan: agent.pause; surface to user with retry_after countdown."},
     detections: [
       {kind: "agent.rate_limited", payload: {agent: "GreenBear", provider: "claude", retry_after_s: 1800}},
       {kind: "caam.no_alternative", payload: {provider: "claude", available_accounts: 1}}
     ],
     wakeAgent: true,
     actionPlan: {actions: [
       {type: "agent.pause", args: {agent: "GreenBear", until: "2026-05-02T20:28:00Z", reason: "claude rate limit; no alternative account"}}
     ]},
     approvalsRequested: [],
     postconditions: [{check: "GreenBear.state == PAUSED", expect: true}],
     activityBehavior: "surface"}' | jq . > "$dir/expected-outcome.json"

  # rate-limited-with-caam variant
  local dir2="$OUT_DIR/rate-limited-with-caam"
  mkdir -p "$dir2/pane-logs" "$dir2/build-logs"
  write_meta "$dir2" "synthetic" "Anthropic rate limit hit; CAAM has alternative account. tend-swarm should propose ActionPlan: caam.switch_account; agent resumes after switch."

  printf '%s' "$BV_TRIAGE_JSON" | jq . > "$dir2/bv-triage.json"
  printf '%s' "$BR_LIST_SLIM_JSON" | jq . > "$dir2/br-list.json"

  jq -cn '{sessions: [{id: "hoopoe-implementation", windows: 1, panes: [
    {id: "%1", agent: "GreenBear", program: "claude-code", model: "claude-opus-4-7", state: "ERROR", last_activity_ts: "2026-05-02T19:58:00Z", bead: "hp-zir", error: "rate_limited", retry_after_s: 1800}
  ]}], counters: {sessions: 1, panes: 1, alive: 1, wedged: 0, errored: 1}}' | jq . > "$dir2/ntm-snapshot.json"

  jq -cn '{messages: [], threads: [], unread_total: 0}' | jq . > "$dir2/agent-mail-dump.json"
  jq -cn '{reservations: [], conflicts: []}' | jq . > "$dir2/reservations.json"

  write_jsonl_events "$dir2" \
    '{"channel":"agent_state","seq":1,"ts":"2026-05-02T19:58:00Z","type":"agent.errored","payload":{"agent":"GreenBear","kind":"rate_limited","retry_after_s":1800}}' \
    '{"channel":"caut","seq":2,"ts":"2026-05-02T19:58:01Z","type":"caut.usage_changed","payload":{"provider":"claude","account":"primary","percent":100,"reset_at":"2026-05-02T20:28:00Z"}}' \
    '{"channel":"caam","seq":3,"ts":"2026-05-02T19:58:02Z","type":"caam.account_status","payload":{"active_account":"primary","available_accounts":3,"alternatives":[{"id":"backup","provider":"claude","percent_used":12}]}}'

  write_pane_log "$dir2" "GreenBear" "[2026-05-02 19:58:00] error: rate_limit_exceeded; retry_after=1800s"
  write_text "$dir2/build-logs/run-005.txt" "(no builds; agent rate-limited; pending switch)"

  write_capabilities "$dir2" '{"caam":{"caam.account.switch":{"status":"ok","notes":"alternative account available"}}}'

  jq -cn \
    '{meta: {kind: "synthetic", scenario: "rate-limited-with-caam", fixturesVersion: "'"$FIXTURES_VERSION"'", capturedAt: "'"$NOW"'", source: "build-scenarios.sh", notes: "Provider rate limit; CAAM alternative available. Pre-script detects; ActionPlan: caam.switch_account; agent resumes after switch."},
     detections: [
       {kind: "agent.rate_limited", payload: {agent: "GreenBear", provider: "claude", retry_after_s: 1800}},
       {kind: "caam.alternative_available", payload: {provider: "claude", alternative: "backup", percent_used: 12}}
     ],
     wakeAgent: true,
     actionPlan: {actions: [
       {type: "caam.switch_account", args: {to: "backup", reason: "primary at 100%"}},
       {type: "agent.resume", args: {agent: "GreenBear", reason: "post-switch retry"}}
     ]},
     approvalsRequested: [
       {scope: "caam.switch_account", reason: "switch primary→backup (autopilot may pre-approve based on §5.3 risk class)"}
     ],
     postconditions: [
       {check: "caam.active_account == backup", expect: true},
       {check: "GreenBear.state in {RUNNING,TYPING,TOOL_USE}", expect: true}
     ],
     activityBehavior: "surface"}' | jq . > "$dir2/expected-outcome.json"
}

# ---- driver ----

while IFS= read -r sc; do
  case "$sc" in
    healthy-hour) build_healthy_hour ;;
    idle-but-not-stuck) build_idle_but_not_stuck ;;
    wedged-pane) build_wedged_pane ;;
    rate-limited-no-caam|rate-limited-with-caam|rate-limited)
      build_rate_limited ;;
    stale-reservation|commit-burst|budget-breach|skill-drift|missing-tool|postcondition-failure|action-arbitration)
      echo "build-scenarios.sh: scenario '$sc' not yet implemented in this seeder; leaving stub" >&2
      ;;
    all)
      build_healthy_hour
      build_idle_but_not_stuck
      build_wedged_pane
      build_rate_limited
      ;;
    *) echo "build-scenarios.sh: unknown scenario: $sc" >&2; exit 2;;
  esac
done < <(scenarios_to_build)

echo "build-scenarios.sh: done; out=$OUT_DIR" >&2
