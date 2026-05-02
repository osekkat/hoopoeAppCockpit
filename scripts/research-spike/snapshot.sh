#!/usr/bin/env bash
# scripts/research-spike/snapshot.sh
#
# Hoopoe Phase 0 research-spike snapshot script (bead hp-6v3).
#
# Produces ONE JSON document on stdout that captures the full ACFS toolchain
# state on a research-spike VPS. The output drives:
#   - parser-fixture corpora (plan.md §16, §18.3, hp-pl5o)
#   - Mock Flywheel Mode (plan.md §13, hp-wle, hp-dr8)
#   - the capability registry (plan.md §2.8, hp-r33)
#   - per-tool integration contracts (hp-78m)
#
# Schema: scripts/research-spike/schema/snapshot.schema.json
#
# Usage (run on a fresh ACFS VPS):
#   ssh <vps> bash -s -- --scenario fresh > snapshot.json < scripts/research-spike/snapshot.sh
#
# Usage (in-place):
#   scripts/research-spike/snapshot.sh --scenario active > snapshot.json
#
# Usage (local self-test on a non-VPS box):
#   scripts/research-spike/snapshot.sh --self-test > snapshot.json
#
# Flags:
#   --scenario {fresh|active|failure|mock|ad_hoc}   default: ad_hoc
#   --scenario-notes <text>                          free-form notes
#   --vps-id <id>                                    default: hostname
#   --output <path>                                  default: stdout
#   --max-bytes <n>                                  per-stream cap, default 1 MiB
#   --no-redact                                      disables ENVELOPE_REDACT
#   --only <tool[,tool...]>                          run only these tools
#   --skip <tool[,tool...]>                          skip these tools
#   --self-test                                      tolerate missing tools, run on any box
#   --schema-validate                                require ajv-cli, validate output before printing
#   --acfs-tag <tag>                                 ACFS pinned tag for meta.acfsTag
#   --fixtures-version <tag>                         meta.fixturesVersion
#   -h, --help                                       this message
#
# Exit codes:
#   0  success
#   1  any tool capture failed in a way the script chose to surface
#       (set HOOPOE_SNAPSHOT_STRICT=1 to elevate non-fatal capture errors)
#   2  envelope/usage error
#   3  schema validation requested but failed
#
# Cardinal rules baked in (per plan.md Appendix C, AGENTS.md):
#   - NEVER run bare `bv` (it launches a TUI). Robot surfaces only.
#   - NEVER run `caam switch-account`, `pt kill`, `sbh cleanup`, `dcg execute` —
#     these are destructive/mutating; we only inspect their --help shape and
#     non-destructive subcommands.
#   - NEVER call provider APIs directly. Tool interrogation only; no LLM reach.
#   - NEVER run `git push` / `git commit` / writes to the repo.

set -euo pipefail

SNAPSHOT_VERSION="0.1.0"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
SCHEMA_RELATIVE="scripts/research-spike/schema/snapshot.schema.json"
LIB_DIR="${SCRIPT_DIR}/lib"

# shellcheck source=lib/envelope.sh
source "${LIB_DIR}/envelope.sh"

# ---- Argument parsing ----
SCENARIO="ad_hoc"
SCENARIO_NOTES=""
VPS_ID=""
OUTPUT_PATH=""
ONLY_LIST=""
SKIP_LIST=""
SELF_TEST=0
SCHEMA_VALIDATE=0
ACFS_TAG=""
FIXTURES_VERSION=""

usage() {
  sed -n '1,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" \
    | grep -E '^#( |$)' \
    | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenario) SCENARIO="$2"; shift 2;;
    --scenario-notes) SCENARIO_NOTES="$2"; shift 2;;
    --vps-id) VPS_ID="$2"; shift 2;;
    --output) OUTPUT_PATH="$2"; shift 2;;
    --max-bytes) ENVELOPE_MAX_BYTES="$2"; export ENVELOPE_MAX_BYTES; shift 2;;
    --no-redact) ENVELOPE_REDACT=0; export ENVELOPE_REDACT; shift;;
    --only) ONLY_LIST="$2"; shift 2;;
    --skip) SKIP_LIST="$2"; shift 2;;
    --self-test) SELF_TEST=1; shift;;
    --schema-validate) SCHEMA_VALIDATE=1; shift;;
    --acfs-tag) ACFS_TAG="$2"; shift 2;;
    --fixtures-version) FIXTURES_VERSION="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "snapshot.sh: unknown flag: $1" >&2; usage >&2; exit 2;;
  esac
done

case "$SCENARIO" in
  fresh|active|failure|mock|ad_hoc) ;;
  *) echo "snapshot.sh: --scenario must be fresh|active|failure|mock|ad_hoc" >&2; exit 2;;
esac

if [[ -z "$VPS_ID" ]]; then
  VPS_ID="$(hostname 2>/dev/null || echo unknown-host)"
fi

# ---- Tool dispatch ----
ALL_TOOLS=(git br bv ntm agent_mail ru health caut caam dcg casr ubs jsm jfp oracle pt srp sbh)

should_run_tool() {
  local t="$1"
  if [[ -n "$ONLY_LIST" ]]; then
    case ",$ONLY_LIST," in *",$t,"*) ;; *) return 1;; esac
  fi
  if [[ -n "$SKIP_LIST" ]]; then
    case ",$SKIP_LIST," in *",$t,"*) return 1;; esac
  fi
  return 0
}

# ---- Output sink (FD 3) ----
SNAPSHOT_TMP="$(mktemp "${TMPDIR:-/tmp}/hoopoe-snapshot.XXXXXX")"
exec 3>"$SNAPSHOT_TMP"
trap 'rm -f "$SNAPSHOT_TMP"' EXIT

START_NS=$(date +%s%N)

# ---- Per-tool captures (defined inline; one function per tool slug) ----
# Each capture::<slug> is responsible for tool_open + invocations + tool_close.
# It must NOT propagate failures of inner commands; surface them inside the
# envelope via record_error/exit codes.

capture::git() {
  envelope::tool_open git
  envelope::probe_version git git || true
  envelope::add_capability git git.status.read ok
  envelope::add_capability git git.diff.read ok
  envelope::add_capability git git.unpushed.list ok "rev-list --left-right" ""
  envelope::add_capability git git.push blocked-by-policy "" "" "snapshot script never pushes"

  envelope::run_capture status_porcelain -- git status --porcelain=v2 --branch
  envelope::run_capture branch_show -- git branch --show-current
  envelope::run_capture head_sha -- git rev-parse HEAD
  envelope::run_capture log_short -- git log -n 10 --pretty=format:'%H %ci %s'
  envelope::run_capture remote -- git remote -v
  envelope::run_capture diff_stat -- git diff --stat HEAD
  envelope::run_capture diff_staged_stat -- git diff --stat --cached
  TAGS=non-fatal envelope::run_capture unpushed -- git log --branches --not --remotes --pretty=format:'%H %s'
  envelope::tool_close git
}

capture::br() {
  envelope::tool_open br
  envelope::probe_version br br || { envelope::tool_close br; return 0; }
  envelope::add_capability br br.issues.read ok
  envelope::add_capability br br.issues.update ok
  envelope::add_capability br br.dep.add ok
  envelope::add_capability br br.sync.flush_only ok "" "" "br sync --flush-only is non-invasive (never runs git)"

  envelope::run_capture help -- br --help
  envelope::run_capture list_open -- br list --status=open --json --limit 250
  envelope::run_capture ready -- br ready --json
  envelope::run_capture cycles -- br dep cycles --json
  envelope::run_capture stats -- br stats --json
  envelope::run_capture schema -- br schema
  envelope::run_capture info -- br info --json
  # .beads/issues.jsonl as a static capture (raw bytes, not via run_capture)
  if [[ -f .beads/issues.jsonl ]]; then
    TAGS=raw-file envelope::run_capture issues_jsonl_head -- head -n 5 .beads/issues.jsonl
    TAGS=raw-file envelope::run_capture issues_jsonl_count -- bash -c 'wc -l < .beads/issues.jsonl'
  fi
  envelope::tool_close br
}

capture::bv() {
  envelope::tool_open bv
  envelope::probe_version bv bv || { envelope::tool_close bv; return 0; }
  envelope::add_capability bv bv.robot.triage ok
  envelope::add_capability bv bv.robot.plan ok
  envelope::add_capability bv bv.robot.insights ok
  envelope::add_capability bv bv.robot.diff ok
  envelope::add_capability bv bv.tui blocked-by-policy "" "" "Guardrail 1: bare bv launches TUI; never invoked"

  TAGS=tui-trap-avoided envelope::run_capture robot_help -- bv --robot-help
  envelope::run_capture robot_recipes -- bv --robot-recipes
  envelope::run_capture robot_triage -- bv --robot-triage
  envelope::run_capture robot_plan -- bv --robot-plan
  envelope::run_capture robot_plan_actionable -- bv --recipe actionable --robot-plan
  envelope::run_capture robot_insights -- bv --robot-insights
  envelope::run_capture robot_priority -- bv --robot-priority
  TAGS=range-may-be-empty envelope::run_capture robot_diff -- bv --robot-diff --diff-since HEAD~30
  envelope::tool_close bv
}

capture::ntm() {
  envelope::tool_open ntm
  envelope::probe_version ntm ntm || { envelope::tool_close ntm; return 0; }
  envelope::add_capability ntm ntm.robot.snapshot ok
  envelope::add_capability ntm ntm.robot.status ok
  envelope::add_capability ntm ntm.robot.tail ok
  envelope::add_capability ntm ntm.serve.rest untested "" "http" "endpoint discovery only"

  envelope::run_capture help -- ntm --help
  TAGS=optional-flag envelope::run_capture robot_help -- ntm --robot-help
  envelope::run_capture sessions_list -- ntm sessions list --json
  envelope::run_capture robot_snapshot -- ntm --robot-snapshot
  envelope::run_capture robot_status -- ntm --robot-status
  TAGS=high-volume envelope::run_capture robot_tail -- ntm --robot-tail --max-bytes 8192
  envelope::tool_close ntm
}

capture::agent_mail() {
  envelope::tool_open agent_mail
  if ! command -v agent_mail >/dev/null 2>&1 \
     && ! command -v agent-mail >/dev/null 2>&1 \
     && ! command -v am >/dev/null 2>&1; then
    envelope::set_tool_meta_bool agent_mail present false
    envelope::set_tool_meta agent_mail skipReason "no agent_mail / agent-mail / am binary on PATH"
    envelope::add_capability agent_mail agent_mail.messages.read missing
    envelope::add_capability agent_mail agent_mail.messages.send missing
    envelope::add_capability agent_mail agent_mail.reservations.list missing
    envelope::tool_close agent_mail
    return 0
  fi
  local bin
  for c in agent_mail agent-mail am; do
    if command -v "$c" >/dev/null 2>&1; then bin="$c"; break; fi
  done
  envelope::probe_version agent_mail "$bin" || true
  envelope::add_capability agent_mail agent_mail.messages.read ok
  envelope::add_capability agent_mail agent_mail.messages.send ok
  envelope::add_capability agent_mail agent_mail.reservations.list ok
  envelope::run_capture help -- "$bin" --help
  envelope::tool_close agent_mail
}

capture::ru() {
  envelope::tool_open ru
  envelope::probe_version ru ru || { envelope::tool_close ru; return 0; }
  envelope::add_capability ru ru.sync.dry_run ok "" "" "--dry-run --non-interactive"
  envelope::add_capability ru ru.status.read ok
  envelope::add_capability ru ru.list.paths ok
  envelope::add_capability ru ru.prune.dry_run ok
  envelope::add_capability ru ru.schema ok

  envelope::run_capture help -- ru --help
  envelope::run_capture schema -- ru --schema
  envelope::run_capture robot_docs -- ru robot-docs
  envelope::run_capture sync_dry_run -- ru sync --dry-run --json --non-interactive
  envelope::run_capture status -- ru status --no-fetch --json
  envelope::run_capture list_paths -- ru list --paths
  envelope::run_capture prune_dry_run -- ru prune --dry-run
  envelope::tool_close ru
}

capture::health() {
  envelope::tool_open health
  envelope::set_tool_meta_bool health present true
  envelope::add_capability health health.coverage untested "" "" "language-specific runners attempted best-effort"
  envelope::add_capability health health.complexity untested

  if command -v lizard >/dev/null 2>&1; then
    envelope::run_capture lizard_version -- lizard --version
    TAGS=may-traverse-large-tree TIMEOUT_S=60 envelope::run_capture lizard_json -- lizard --xml
  else
    envelope::record_error health "lizard not on PATH"
  fi
  if command -v scc >/dev/null 2>&1; then
    envelope::run_capture scc -- scc --format json --no-cocomo
  fi
  if command -v tokei >/dev/null 2>&1; then
    envelope::run_capture tokei -- tokei --output json
  fi
  envelope::tool_close health
}

capture::caut() {
  envelope::tool_open caut
  envelope::probe_version caut caut || { envelope::tool_close caut; return 0; }
  envelope::add_capability caut caut.usage.snapshot ok
  envelope::run_capture help -- caut --help
  envelope::run_capture usage_json -- caut usage --json
  envelope::run_capture status -- caut status --json
  envelope::tool_close caut
}

capture::caam() {
  envelope::tool_open caam
  envelope::probe_version caam caam || { envelope::tool_close caam; return 0; }
  envelope::add_capability caam caam.accounts.list ok
  envelope::add_capability caam caam.account.switch blocked-by-policy "" "" "destructive; --help only in snapshot"
  envelope::run_capture help -- caam --help
  envelope::run_capture accounts_list -- caam account-list --json
  envelope::run_capture account_status -- caam account-status --json
  TAGS=destructive-help-only envelope::run_capture switch_help -- caam switch-account --help
  envelope::tool_close caam
}

capture::dcg() {
  envelope::tool_open dcg
  envelope::probe_version dcg dcg || { envelope::tool_close dcg; return 0; }
  envelope::add_capability dcg dcg.verdicts.subscribe untested "" "" "verdict format pinned via --help; runtime stream from hook"
  envelope::run_capture help -- dcg --help
  envelope::run_capture status -- dcg status --json
  envelope::tool_close dcg
}

capture::casr() {
  envelope::tool_open casr
  envelope::probe_version casr casr || { envelope::tool_close casr; return 0; }
  envelope::add_capability casr casr.session.resume untested
  envelope::run_capture help -- casr --help
  envelope::run_capture status -- casr status --json
  envelope::tool_close casr
}

capture::ubs() {
  envelope::tool_open ubs
  envelope::probe_version ubs ubs || { envelope::tool_close ubs; return 0; }
  envelope::add_capability ubs ubs.scan ok
  envelope::run_capture help -- ubs --help
  TAGS=scan-may-be-slow TIMEOUT_S=60 envelope::run_capture self_scan -- ubs --ci .
  envelope::tool_close ubs
}

capture::jsm() {
  envelope::tool_open jsm
  envelope::probe_version jsm jsm || { envelope::tool_close jsm; return 0; }
  envelope::add_capability jsm jsm.skill.install ok
  envelope::add_capability jsm jsm.skill.verify ok "" "" "SHA-256 deterministic"
  envelope::add_capability jsm jsm.skill.list ok
  envelope::run_capture help -- jsm --help
  envelope::run_capture list -- jsm list --json
  envelope::run_capture verify_help -- jsm verify --help
  envelope::tool_close jsm
}

capture::jfp() {
  envelope::tool_open jfp
  envelope::probe_version jfp jfp || { envelope::tool_close jfp; return 0; }
  envelope::add_capability jfp jfp.skill.install ok "" "" "free fallback for jsm"
  envelope::add_capability jfp jfp.skill.list ok
  envelope::run_capture help -- jfp --help
  envelope::run_capture list -- jfp list --json
  envelope::tool_close jfp
}

capture::oracle() {
  envelope::tool_open oracle
  envelope::probe_version oracle oracle || { envelope::tool_close oracle; return 0; }
  envelope::add_capability oracle oracle.serve.status ok
  envelope::add_capability oracle oracle.browser.run untested "" "" "ChatGPT Pro web; only on macOS user host"
  envelope::run_capture help -- oracle --help
  envelope::run_capture serve_help -- oracle serve --help
  envelope::run_capture remote_help -- oracle --remote-host --help
  envelope::tool_close oracle
}

capture::pt() {
  envelope::tool_open pt
  envelope::probe_version pt pt || { envelope::tool_close pt; return 0; }
  envelope::add_capability pt pt.kill blocked-by-policy "" "" "destructive; --help only in snapshot"
  envelope::run_capture help -- pt --help
  envelope::run_capture list -- pt list --json
  envelope::tool_close pt
}

capture::srp() {
  envelope::tool_open srp
  envelope::probe_version srp srp || { envelope::tool_close srp; return 0; }
  envelope::add_capability srp srp.signals.read ok
  envelope::run_capture help -- srp --help
  envelope::run_capture signals -- srp signals --json
  envelope::tool_close srp
}

capture::sbh() {
  envelope::tool_open sbh
  envelope::probe_version sbh sbh || { envelope::tool_close sbh; return 0; }
  envelope::add_capability sbh sbh.cleanup blocked-by-policy "" "" "destructive; --help only in snapshot"
  envelope::add_capability sbh sbh.status ok
  envelope::run_capture help -- sbh --help
  envelope::run_capture status -- sbh status --json
  envelope::tool_close sbh
}

# ---- Drive captures ----
for tool in "${ALL_TOOLS[@]}"; do
  if ! should_run_tool "$tool"; then
    continue
  fi
  if "capture::${tool}"; then
    :
  else
    rc=$?
    echo "snapshot.sh: capture::${tool} failed (rc=${rc})" >&2
    if [[ "${HOOPOE_SNAPSHOT_STRICT:-0}" == "1" ]]; then
      exit 1
    fi
  fi
done

exec 3>&-

END_NS=$(date +%s%N)
DURATION_MS=$(( (END_NS - START_NS) / 1000000 ))

# ---- Meta block ----
NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
HOST_UNAME="$(uname -a 2>/dev/null || echo unknown)"
HOST_LSB=""
if command -v lsb_release >/dev/null 2>&1; then
  HOST_LSB="$(lsb_release -d 2>/dev/null | sed 's/^Description:\s*//')"
elif [[ -f /etc/os-release ]]; then
  HOST_LSB="$(. /etc/os-release; echo "$PRETTY_NAME")"
fi
HOST_KERNEL="$(uname -r 2>/dev/null || echo unknown)"
HOST_CPU="$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || echo null)"
HOST_MEM_KB="$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo null)"
HOST_DF="$(df -h / 2>/dev/null | awk 'NR==2{print}')"

if [[ "$HOST_CPU" == "" ]]; then HOST_CPU=null; fi
if [[ "$HOST_MEM_KB" == "" ]]; then HOST_MEM_KB=null; fi

# Tool-version map distilled from the per-tool envelopes.
TOOL_VERSIONS_PATH="$(mktemp "${TMPDIR:-/tmp}/hoopoe-snapshot-versions.XXXXXX")"
trap 'rm -f "$SNAPSHOT_TMP" "$TOOL_VERSIONS_PATH" "$META_PATH" "$CAPTURES_PATH" "$FINAL_PATH"' EXIT
jq -cs 'map({(.tool): (.version // null)}) | add // {}' "$SNAPSHOT_TMP" > "$TOOL_VERSIONS_PATH"

META_PATH="$(mktemp "${TMPDIR:-/tmp}/hoopoe-snapshot-meta.XXXXXX")"
jq -c \
  --arg sv "$SNAPSHOT_VERSION" \
  --arg url "$SCHEMA_RELATIVE" \
  --arg now "$NOW_ISO" \
  --arg vps "$VPS_ID" \
  --arg scenario "$SCENARIO" \
  --arg notes "$SCENARIO_NOTES" \
  --arg acfs "$ACFS_TAG" \
  --arg fix "$FIXTURES_VERSION" \
  --arg uname "$HOST_UNAME" \
  --arg lsb "$HOST_LSB" \
  --arg kernel "$HOST_KERNEL" \
  --arg df "$HOST_DF" \
  --argjson cpu "$HOST_CPU" \
  --argjson mem "$HOST_MEM_KB" \
  --argjson dur "$DURATION_MS" \
  '{
    snapshotVersion: $sv,
    snapshotSchemaUrl: $url,
    capturedAt: $now,
    vpsId: $vps,
    scenario: $scenario,
    host: {
      uname: $uname,
      lsbRelease: (if $lsb == "" then null else $lsb end),
      kernel: $kernel,
      cpuCount: $cpu,
      memTotalKb: $mem,
      diskFree: (if $df == "" then null else $df end)
    },
    toolVersions: .,
    captureDurationMs: $dur
  }
  + (if $notes == "" then {} else {scenarioNotes: $notes} end)
  + (if $acfs == "" then {} else {acfsTag: $acfs} end)
  + (if $fix == "" then {} else {fixturesVersion: $fix} end)' \
  "$TOOL_VERSIONS_PATH" > "$META_PATH"

# Captures: ndjson → {slug: ToolCapture}
CAPTURES_PATH="$(mktemp "${TMPDIR:-/tmp}/hoopoe-snapshot-captures.XXXXXX")"
jq -cs 'map({(.tool): .}) | add // {}' "$SNAPSHOT_TMP" > "$CAPTURES_PATH"

FINAL_PATH="$(mktemp "${TMPDIR:-/tmp}/hoopoe-snapshot-final.XXXXXX")"
jq -c -n \
  --slurpfile meta "$META_PATH" \
  --slurpfile captures "$CAPTURES_PATH" \
  '{meta: $meta[0], captures: $captures[0]}' > "$FINAL_PATH"

# ---- Schema validation (optional) ----
if [[ "$SCHEMA_VALIDATE" == "1" ]]; then
  if ! command -v ajv >/dev/null 2>&1; then
    echo "snapshot.sh: --schema-validate requires ajv-cli (npm i -g ajv-cli ajv-formats)" >&2
    exit 3
  fi
  if ! ajv validate -s "${SCRIPT_DIR}/schema/snapshot.schema.json" -d "$FINAL_PATH" --spec=draft2020 -c ajv-formats >/dev/null 2>&1; then
    echo "snapshot.sh: schema validation failed" >&2
    ajv validate -s "${SCRIPT_DIR}/schema/snapshot.schema.json" -d "$FINAL_PATH" --spec=draft2020 -c ajv-formats >&2 || true
    exit 3
  fi
fi

# Pretty-print for human review unless HOOPOE_SNAPSHOT_COMPACT=1.
if [[ -n "$OUTPUT_PATH" ]]; then
  if [[ "${HOOPOE_SNAPSHOT_COMPACT:-0}" == "1" ]]; then
    cp "$FINAL_PATH" "$OUTPUT_PATH"
  else
    jq . "$FINAL_PATH" > "$OUTPUT_PATH"
  fi
  echo "snapshot.sh: wrote $(wc -c <"$OUTPUT_PATH" | tr -d ' ') bytes to $OUTPUT_PATH" >&2
else
  if [[ "${HOOPOE_SNAPSHOT_COMPACT:-0}" == "1" ]]; then
    cat "$FINAL_PATH"
  else
    jq . "$FINAL_PATH"
  fi
fi
