#!/usr/bin/env bash
# Hoopoe Phase 0 real-VPS evidence pack collector (hp-vtwm).
#
# This script is scaffolding for the human-gated Phase 0 acceptance pass. It
# never provisions a VPS and never installs ACFS. Run it against an already
# provisioned, ACFS-installed research VPS after hp-r7i / hp-jvm / hp-7cs are
# available.

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." >/dev/null 2>&1 && pwd)"
SNAPSHOT_SH="${SCRIPT_DIR}/snapshot.sh"

PACK_VERSION="0.1.0"
FIXTURES_VERSION="phase0-2026-05-02"
OUTPUT_DIR="/tmp"
LANDING_DIR=""
PROJECT_DIR=""
VPS_ID=""
ACFS_TAG=""
WIZARD_COMMAND=""
FRESH_PREPARE_CMD=""
ACTIVE_PREPARE_CMD=""
FAILURE_PREPARE_CMD=""
SCENARIO_NOTES_FRESH=""
SCENARIO_NOTES_ACTIVE=""
SCENARIO_NOTES_FAILURE=""
MOCK_CORPUS_DIR=""
SSH_TARGET=""
REMOTE_MODE=0
REMOTE_WORK_DIR=""

SCENARIOS=(fresh active failure)
VERSION_TOOLS=(acfs git br bv ntm agent_mail rch caam dcg caut casr srp sbh pt ubs jsm jfp ru health oracle)
ADAPTER_TOOLS=(git br bv ntm agent_mail rch caam dcg caut casr srp sbh pt ubs jsm jfp ru health oracle)

usage() {
  cat <<'USAGE'
Usage:
  collect-evidence-pack.sh [options]

Direct run on the VPS:
  scripts/research-spike/collect-evidence-pack.sh \
    --project-dir /data/projects/<project> \
    --vps-id research-spike-2026-05-02 \
    --acfs-tag <pinned-acfs-tag> \
    --wizard-command '<13-step wizard command or wrapper>'

Run via SSH from this workstation:
  scripts/research-spike/collect-evidence-pack.sh \
    --ssh user@host \
    --project-dir /data/projects/<project> \
    --landing-dir packages/fixtures/phase0-2026-05-02/incoming \
    --wizard-command '<13-step wizard command or wrapper>'

Mock mechanical test:
  scripts/research-spike/collect-evidence-pack.sh \
    --mock-corpus-dir packages/fixtures/scenarios/healthy-hour \
    --output-dir /tmp

Options:
  --project-dir <path>           Project directory on the VPS. Defaults to cwd.
  --output-dir <path>            Where /tmp-style pack directory + tar.zst land. Default: /tmp.
  --landing-dir <path>           Optional local landing dir. Direct mode copies the tar there;
                                 --ssh mode rsyncs/scps the remote tar there.
  --vps-id <id>                  Stable VPS identifier. Defaults to hostname.
  --acfs-tag <tag>               Pinned ACFS version/tag for manifest + snapshot metadata.
  --fixtures-version <tag>       Fixture version tag. Default: phase0-2026-05-02.
  --wizard-command <cmd>         Command/wrapper that performs the 13-step wizard run-through.
  --fresh-prepare-cmd <cmd>      Optional command to prepare the fresh scenario before snapshot.
  --active-prepare-cmd <cmd>     Optional command to prepare the active scenario before snapshot.
  --failure-prepare-cmd <cmd>    Optional command to prepare the failure scenario before snapshot.
  --fresh-notes <text>           Scenario notes written into snapshot metadata.
  --active-notes <text>          Scenario notes written into snapshot metadata.
  --failure-notes <text>         Scenario notes written into snapshot metadata.
  --mock-corpus-dir <path>       Use a Mock Flywheel scenario corpus for mechanical CI tests.
  --ssh <target>                 Upload this script bundle and run collection on target via ssh.
  --remote-work-dir <path>       Remote staging dir for --ssh. Default: /tmp/hoopoe-phase0-collector-<timestamp>.
  -h, --help                     Show this help.

Output:
  /tmp/hoopoe-phase0-evidence-<UTC-timestamp>.tar.zst

The tarball is intentionally append-only and timestamped so reruns are safe.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project-dir) PROJECT_DIR="$2"; shift 2;;
    --output-dir) OUTPUT_DIR="$2"; shift 2;;
    --landing-dir) LANDING_DIR="$2"; shift 2;;
    --vps-id) VPS_ID="$2"; shift 2;;
    --acfs-tag) ACFS_TAG="$2"; shift 2;;
    --fixtures-version) FIXTURES_VERSION="$2"; shift 2;;
    --wizard-command) WIZARD_COMMAND="$2"; shift 2;;
    --fresh-prepare-cmd) FRESH_PREPARE_CMD="$2"; shift 2;;
    --active-prepare-cmd) ACTIVE_PREPARE_CMD="$2"; shift 2;;
    --failure-prepare-cmd) FAILURE_PREPARE_CMD="$2"; shift 2;;
    --fresh-notes) SCENARIO_NOTES_FRESH="$2"; shift 2;;
    --active-notes) SCENARIO_NOTES_ACTIVE="$2"; shift 2;;
    --failure-notes) SCENARIO_NOTES_FAILURE="$2"; shift 2;;
    --mock-corpus-dir) MOCK_CORPUS_DIR="$2"; shift 2;;
    --ssh) SSH_TARGET="$2"; shift 2;;
    --remote-mode) REMOTE_MODE=1; shift;;
    --remote-work-dir) REMOTE_WORK_DIR="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "collect-evidence-pack.sh: unknown flag: $1" >&2; usage >&2; exit 2;;
  esac
done

require_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "collect-evidence-pack.sh: required binary missing: $1" >&2
    exit 2
  }
}

json_string_array() {
  jq -R . | jq -s .
}

iso_now() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

timestamp() {
  date -u +%Y%m%dT%H%M%S%NZ
}

quote_arg() {
  printf '%q' "$1"
}

run_via_ssh() {
  require_bin ssh
  local ts remote_dir remote_cmd remote_output remote_tar
  ts="$(timestamp)"
  remote_dir="${REMOTE_WORK_DIR:-/tmp/hoopoe-phase0-collector-${ts}}"
  ssh "$SSH_TARGET" "mkdir -p $(quote_arg "$remote_dir")"
  tar -cf - -C "$REPO_ROOT" scripts/research-spike \
    | ssh "$SSH_TARGET" "tar -xf - -C $(quote_arg "$remote_dir")"

  local args=(
    "--remote-mode"
    "--project-dir" "${PROJECT_DIR:-.}"
    "--output-dir" "$OUTPUT_DIR"
    "--fixtures-version" "$FIXTURES_VERSION"
  )
  [[ -n "$VPS_ID" ]] && args+=("--vps-id" "$VPS_ID")
  [[ -n "$ACFS_TAG" ]] && args+=("--acfs-tag" "$ACFS_TAG")
  [[ -n "$WIZARD_COMMAND" ]] && args+=("--wizard-command" "$WIZARD_COMMAND")
  [[ -n "$FRESH_PREPARE_CMD" ]] && args+=("--fresh-prepare-cmd" "$FRESH_PREPARE_CMD")
  [[ -n "$ACTIVE_PREPARE_CMD" ]] && args+=("--active-prepare-cmd" "$ACTIVE_PREPARE_CMD")
  [[ -n "$FAILURE_PREPARE_CMD" ]] && args+=("--failure-prepare-cmd" "$FAILURE_PREPARE_CMD")
  [[ -n "$SCENARIO_NOTES_FRESH" ]] && args+=("--fresh-notes" "$SCENARIO_NOTES_FRESH")
  [[ -n "$SCENARIO_NOTES_ACTIVE" ]] && args+=("--active-notes" "$SCENARIO_NOTES_ACTIVE")
  [[ -n "$SCENARIO_NOTES_FAILURE" ]] && args+=("--failure-notes" "$SCENARIO_NOTES_FAILURE")

  local quoted_args=()
  local arg
  for arg in "${args[@]}"; do
    quoted_args+=("$(quote_arg "$arg")")
  done

  remote_cmd="cd $(quote_arg "${PROJECT_DIR:-.}") && bash $(quote_arg "${remote_dir}/scripts/research-spike/collect-evidence-pack.sh") ${quoted_args[*]}"
  remote_output="$(ssh "$SSH_TARGET" "$remote_cmd")"
  printf '%s\n' "$remote_output"
  remote_tar="$(printf '%s\n' "$remote_output" | awk -F= '/^EVIDENCE_TAR=/{print $2}' | tail -n 1)"
  if [[ -n "$LANDING_DIR" && -n "$remote_tar" ]]; then
    mkdir -p "$LANDING_DIR"
    if command -v rsync >/dev/null 2>&1; then
      rsync -azP "${SSH_TARGET}:${remote_tar}" "${LANDING_DIR}/"
    else
      scp "${SSH_TARGET}:${remote_tar}" "${LANDING_DIR}/"
    fi
  fi
}

if [[ -n "$SSH_TARGET" && "$REMOTE_MODE" != "1" ]]; then
  run_via_ssh
  exit 0
fi

require_bin jq
require_bin tar
require_bin zstd

if [[ ! -x "$SNAPSHOT_SH" ]]; then
  echo "collect-evidence-pack.sh: snapshot.sh is missing or not executable: $SNAPSHOT_SH" >&2
  exit 2
fi

if [[ -z "$PROJECT_DIR" ]]; then
  PROJECT_DIR="$(pwd)"
fi
if [[ -z "$VPS_ID" ]]; then
  VPS_ID="$(hostname 2>/dev/null || echo unknown-host)"
fi

MODE="real-vps"
REAL_VPS_ACCEPTANCE=true
if [[ -n "$MOCK_CORPUS_DIR" ]]; then
  MODE="mock"
  REAL_VPS_ACCEPTANCE=false
  MOCK_CORPUS_DIR="$(cd -- "$MOCK_CORPUS_DIR" >/dev/null 2>&1 && pwd)"
fi

UTC_TIMESTAMP="$(timestamp)"
PACK_NAME="hoopoe-phase0-evidence-${UTC_TIMESTAMP}"
PACK_DIR="${OUTPUT_DIR%/}/${PACK_NAME}"
TAR_PATH="${OUTPUT_DIR%/}/${PACK_NAME}.tar.zst"

if [[ -e "$PACK_DIR" || -e "$TAR_PATH" ]]; then
  echo "collect-evidence-pack.sh: output already exists for timestamp ${UTC_TIMESTAMP}" >&2
  exit 2
fi

mkdir -p \
  "$PACK_DIR/versions" \
  "$PACK_DIR/acfs" \
  "$PACK_DIR/wizard" \
  "$PACK_DIR/logs"
for scenario in "${SCENARIOS[@]}"; do
  mkdir -p "$PACK_DIR/scenarios/$scenario/adapters"
done

tool_binary_candidates() {
  case "$1" in
    agent_mail) printf '%s\n' agent_mail agent-mail am;;
    health) printf '%s\n' lizard scc tokei;;
    *) printf '%s\n' "$1";;
  esac
}

resolve_tool_bin() {
  local tool="$1" candidate
  while IFS= read -r candidate; do
    if command -v "$candidate" >/dev/null 2>&1; then
      command -v "$candidate"
      return 0
    fi
  done < <(tool_binary_candidates "$tool")
  return 1
}

capture_version_text() {
  local bin="$1" output rc
  local timeout_s="${HOOPOE_COLLECT_VERSION_TIMEOUT_S:-10}"
  for flag in --version version -V --help; do
    set +e
    output="$(timeout "${timeout_s}s" "$bin" "$flag" 2>&1 | head -n 3)"
    rc=$?
    set -e
    if [[ "$rc" -eq 0 && -n "$output" ]]; then
      printf '%s\n' "$output"
      return 0
    fi
  done
  printf 'version unavailable\n'
}

capture_versions() {
  local ndjson="$PACK_DIR/versions/tool-versions.ndjson"
  : > "$ndjson"
  local tool bin version present
  for tool in "${VERSION_TOOLS[@]}"; do
    if bin="$(resolve_tool_bin "$tool")"; then
      present=true
      version="$(capture_version_text "$bin")"
    else
      present=false
      bin=""
      version=""
    fi
    jq -cn \
      --arg tool "$tool" \
      --arg bin "$bin" \
      --arg version "$version" \
      --argjson present "$present" \
      '{tool: $tool, present: $present, binPath: (if $bin == "" then null else $bin end), version: (if $version == "" then null else $version end)}' \
      >> "$ndjson"
  done
  jq -s '{capturedAt: $capturedAt, tools: .}' --arg capturedAt "$(iso_now)" "$ndjson" \
    > "$PACK_DIR/versions/tool-versions.json"
}

capture_acfs_doctor() {
  local status_path="$PACK_DIR/acfs/doctor-status.json"
  if command -v acfs >/dev/null 2>&1; then
    set +e
    acfs doctor --json > "$PACK_DIR/acfs/doctor.json" 2> "$PACK_DIR/acfs/doctor.stderr"
    local rc=$?
    set -e
    jq -n --argjson exit "$rc" --arg capturedAt "$(iso_now)" \
      '{present: true, exit: $exit, capturedAt: $capturedAt}' > "$status_path"
  else
    jq -n --arg capturedAt "$(iso_now)" \
      '{present: false, exit: null, capturedAt: $capturedAt, skipReason: "acfs binary not found on PATH"}' \
      > "$status_path"
    jq -n '{present: false, error: "acfs binary not found on PATH"}' \
      > "$PACK_DIR/acfs/doctor.json"
    : > "$PACK_DIR/acfs/doctor.stderr"
  fi
}

run_transcript_command() {
  local label="$1" command="$2" out_dir="$3"
  mkdir -p "$out_dir"
  printf '%s\n' "$command" > "$out_dir/command.txt"

  if [[ -z "$command" ]]; then
    jq -n --arg label "$label" --arg capturedAt "$(iso_now)" \
      '{label: $label, capturedAt: $capturedAt, skipped: true, reason: "no command supplied"}' \
      > "$out_dir/status.json"
    printf 'No wizard command supplied. Human VPS run must provide --wizard-command.\n' \
      > "$out_dir/transcript.txt"
    return 0
  fi

  local rc
  set +e
  if command -v script >/dev/null 2>&1; then
    script -q -e -c "$command" "$out_dir/transcript.typescript" \
      > "$out_dir/stdout.txt" 2> "$out_dir/stderr.txt"
    rc=$?
  else
    bash -lc "$command" > "$out_dir/stdout.txt" 2> "$out_dir/stderr.txt"
    rc=$?
    {
      printf '$ %s\n' "$command"
      printf '\n--- stdout ---\n'
      cat "$out_dir/stdout.txt"
      printf '\n--- stderr ---\n'
      cat "$out_dir/stderr.txt"
    } > "$out_dir/transcript.txt"
  fi
  set -e

  jq -n --arg label "$label" --arg capturedAt "$(iso_now)" --argjson exit "$rc" \
    '{label: $label, capturedAt: $capturedAt, skipped: false, exit: $exit}' \
    > "$out_dir/status.json"
}

scenario_prepare_command() {
  case "$1" in
    fresh) printf '%s\n' "$FRESH_PREPARE_CMD";;
    active) printf '%s\n' "$ACTIVE_PREPARE_CMD";;
    failure) printf '%s\n' "$FAILURE_PREPARE_CMD";;
  esac
}

scenario_notes() {
  case "$1" in
    fresh) printf '%s\n' "$SCENARIO_NOTES_FRESH";;
    active) printf '%s\n' "$SCENARIO_NOTES_ACTIVE";;
    failure) printf '%s\n' "$SCENARIO_NOTES_FAILURE";;
  esac
}

mock_fixture_for_tool() {
  local tool="$1"
  case "$tool" in
    br) printf '%s\n' "$MOCK_CORPUS_DIR/br-list.json";;
    bv) printf '%s\n' "$MOCK_CORPUS_DIR/bv-triage.json";;
    ntm) printf '%s\n' "$MOCK_CORPUS_DIR/ntm-snapshot.json";;
    agent_mail) printf '%s\n' "$MOCK_CORPUS_DIR/agent-mail-dump.json";;
    health|ru|caut|caam|dcg|casr|ubs|jsm|jfp|oracle|pt|srp|sbh|rch) printf '%s\n' "$MOCK_CORPUS_DIR/capabilities.json";;
    git) printf '%s\n' "$MOCK_CORPUS_DIR/meta.json";;
    *) return 1;;
  esac
}

write_adapter_files() {
  local scenario="$1" snapshot_path="$2" scenario_dir="$3" tool src
  for tool in "${ADAPTER_TOOLS[@]}"; do
    if [[ -n "$MOCK_CORPUS_DIR" ]] && src="$(mock_fixture_for_tool "$tool")" && [[ -f "$src" ]]; then
      jq -n \
        --arg tool "$tool" \
        --arg scenario "$scenario" \
        --arg source "mock-flywheel-corpus" \
        --arg fixtureFile "$(basename "$src")" \
        --slurpfile payload "$src" \
        '{tool: $tool, scenario: $scenario, present: true, source: $source, fixtureFile: $fixtureFile, payload: $payload[0]}' \
        > "$scenario_dir/adapters/$tool.json"
    elif jq -e --arg tool "$tool" '.captures[$tool] != null' "$snapshot_path" >/dev/null 2>&1; then
      jq --arg tool "$tool" '.captures[$tool]' "$snapshot_path" > "$scenario_dir/adapters/$tool.json"
    else
      jq -n --arg tool "$tool" --arg scenario "$scenario" --arg mode "$MODE" \
        '{tool: $tool, scenario: $scenario, present: false, source: "collector", mode: $mode, skipReason: "tool not captured by snapshot.sh"}' \
        > "$scenario_dir/adapters/$tool.json"
    fi
  done

  printf '%s\n' "${ADAPTER_TOOLS[@]}" | json_string_array \
    | jq --arg scenario "$scenario" --arg mode "$MODE" '{scenario: $scenario, mode: $mode, adapters: .}' \
    > "$scenario_dir/adapter-index.json"
}

run_snapshot_for_scenario() {
  local scenario="$1" scenario_dir="$PACK_DIR/scenarios/$scenario"
  local prepare_cmd notes snapshot_flags
  prepare_cmd="$(scenario_prepare_command "$scenario")"
  notes="$(scenario_notes "$scenario")"

  run_transcript_command "prepare-${scenario}" "$prepare_cmd" "$scenario_dir/prepare"

  snapshot_flags=(
    "--scenario" "$scenario"
    "--vps-id" "$VPS_ID"
    "--fixtures-version" "$FIXTURES_VERSION"
    "--output" "$scenario_dir/snapshot.json"
  )
  [[ -n "$ACFS_TAG" ]] && snapshot_flags+=("--acfs-tag" "$ACFS_TAG")
  [[ -n "$notes" ]] && snapshot_flags+=("--scenario-notes" "$notes")

  if [[ "$MODE" == "mock" ]]; then
    snapshot_flags+=("--self-test" "--only" "git")
  fi

  (
    cd "$PROJECT_DIR"
    HOOPOE_SNAPSHOT_COMPACT=1 bash "$SNAPSHOT_SH" "${snapshot_flags[@]}" \
      > "$scenario_dir/snapshot.stdout" \
      2> "$scenario_dir/snapshot.stderr"
  )

  write_adapter_files "$scenario" "$scenario_dir/snapshot.json" "$scenario_dir"
}

copy_mock_corpus() {
  if [[ -z "$MOCK_CORPUS_DIR" ]]; then
    return 0
  fi
  mkdir -p "$PACK_DIR/mock-corpus"
  jq -n \
    --arg sourceDir "$MOCK_CORPUS_DIR" \
    --arg capturedAt "$(iso_now)" \
    '{sourceDir: $sourceDir, capturedAt: $capturedAt, note: "Mock mode records only safe metadata here; adapter payloads are copied into scenarios/*/adapters/*.json. Raw .goldens are intentionally excluded because they contain loud mock token fixtures."}' \
    > "$PACK_DIR/mock-corpus/source.json"
  for safe_file in meta.json capabilities.json expected-outcome.json; do
    if [[ -f "$MOCK_CORPUS_DIR/$safe_file" ]]; then
      cp "$MOCK_CORPUS_DIR/$safe_file" "$PACK_DIR/mock-corpus/$safe_file"
    fi
  done
}

write_manifest() {
  local scenarios_json adapters_json versions_json
  scenarios_json="$(printf '%s\n' "${SCENARIOS[@]}" | json_string_array)"
  adapters_json="$(printf '%s\n' "${ADAPTER_TOOLS[@]}" | json_string_array)"
  versions_json="$(printf '%s\n' "${VERSION_TOOLS[@]}" | json_string_array)"

  jq -n \
    --arg packVersion "$PACK_VERSION" \
    --arg createdAt "$(iso_now)" \
    --arg mode "$MODE" \
    --arg fixturesVersion "$FIXTURES_VERSION" \
    --arg vpsId "$VPS_ID" \
    --arg projectDir "$PROJECT_DIR" \
    --arg acfsTag "$ACFS_TAG" \
    --arg mockCorpus "$MOCK_CORPUS_DIR" \
    --argjson realVpsAcceptance "$REAL_VPS_ACCEPTANCE" \
    --argjson scenarios "$scenarios_json" \
    --argjson adapters "$adapters_json" \
    --argjson versionTools "$versions_json" \
    '{
      packVersion: $packVersion,
      createdAt: $createdAt,
      mode: $mode,
      realVpsAcceptance: $realVpsAcceptance,
      fixturesVersion: $fixturesVersion,
      vpsId: $vpsId,
      projectDir: $projectDir,
      scenarios: $scenarios,
      adapterTools: $adapters,
      versionTools: $versionTools,
      humanGatedBeads: ["hp-r7i", "hp-jvm", "hp-7cs"],
      closePolicy: "Close hp-r7i, hp-jvm, hp-7cs, then hp-vtwm only after a verifier-passing real-VPS pack lands in packages/fixtures/phase0-2026-05-02/."
    }
    + (if $acfsTag == "" then {} else {acfsTag: $acfsTag} end)
    + (if $mockCorpus == "" then {} else {mockCorpusDir: $mockCorpus} end)' \
    > "$PACK_DIR/manifest.json"
}

capture_versions
capture_acfs_doctor
run_transcript_command "wizard-13-step" "$WIZARD_COMMAND" "$PACK_DIR/wizard"
copy_mock_corpus

for scenario in "${SCENARIOS[@]}"; do
  run_snapshot_for_scenario "$scenario"
done

write_manifest

tar -I zstd -cf "$TAR_PATH" -C "$OUTPUT_DIR" "$PACK_NAME"
sha256sum "$TAR_PATH" > "$TAR_PATH.sha256"

if [[ -n "$LANDING_DIR" ]]; then
  mkdir -p "$LANDING_DIR"
  cp "$TAR_PATH" "$TAR_PATH.sha256" "$LANDING_DIR/"
fi

printf 'EVIDENCE_DIR=%s\n' "$PACK_DIR"
printf 'EVIDENCE_TAR=%s\n' "$TAR_PATH"
