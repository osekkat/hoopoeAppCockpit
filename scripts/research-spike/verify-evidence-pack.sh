#!/usr/bin/env bash
# Verify a Hoopoe Phase 0 evidence pack without extracting it into the repo.

set -euo pipefail

TAR_PATH=""

usage() {
  cat <<'USAGE'
Usage:
  verify-evidence-pack.sh /tmp/hoopoe-phase0-evidence-<timestamp>.tar.zst

Checks:
  - required manifest, version, ACFS doctor, wizard transcript, scenario, and
    adapter artifacts are present;
  - fresh / active / failure each include snapshot.json and per-adapter JSON;
  - known credential/token/key shapes do not appear in text payloads.

Exit:
  0 PASS
  1 FAIL
USAGE
}

if [[ $# -ne 1 || "$1" == "-h" || "$1" == "--help" ]]; then
  usage
  exit $([[ $# -eq 1 ]] && [[ "$1" =~ ^-h|--help$ ]] && echo 0 || echo 2)
fi

TAR_PATH="$1"

require_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "verify-evidence-pack.sh: required binary missing: $1" >&2
    exit 2
  }
}

require_bin jq
require_bin tar
require_bin zstd

if [[ ! -f "$TAR_PATH" ]]; then
  echo "FAIL evidence pack not found: $TAR_PATH" >&2
  exit 1
fi

SCENARIOS=(fresh active failure)
ADAPTER_TOOLS=(git br bv ntm agent_mail rch caam dcg caut casr srp sbh pt ubs jsm jfp ru health oracle)
FAILURES=0
CHECKS=0
LIST_PATH="$(mktemp "${TMPDIR:-/tmp}/hoopoe-evidence-list.XXXXXX")"

tar_list() {
  tar -I zstd -tf "$TAR_PATH"
}

tar_cat() {
  tar -I zstd -xOf "$TAR_PATH" "$1"
}

report() {
  local status="$1" label="$2" detail="${3:-}"
  CHECKS=$((CHECKS + 1))
  if [[ "$status" == "PASS" ]]; then
    printf '[PASS] %s%s\n' "$label" "${detail:+ -- $detail}"
  else
    FAILURES=$((FAILURES + 1))
    printf '[FAIL] %s%s\n' "$label" "${detail:+ -- $detail}"
  fi
}

tar_list > "$LIST_PATH"
ROOT="$(head -n 1 "$LIST_PATH" | cut -d/ -f1)"

has_entry() {
  local path="$1"
  grep -qxF "${ROOT}/${path}" "$LIST_PATH"
}

check_present() {
  local path="$1"
  if has_entry "$path"; then
    report PASS "$path present"
  else
    report FAIL "$path present"
  fi
}

check_present "manifest.json"
check_present "versions/tool-versions.json"
check_present "acfs/doctor.json"
check_present "acfs/doctor-status.json"
if has_entry "wizard/transcript.typescript" || has_entry "wizard/transcript.txt"; then
  report PASS "wizard transcript present"
else
  report FAIL "wizard transcript present"
fi
check_present "wizard/status.json"

for scenario in "${SCENARIOS[@]}"; do
  check_present "scenarios/${scenario}/snapshot.json"
  check_present "scenarios/${scenario}/adapter-index.json"
  check_present "scenarios/${scenario}/prepare/status.json"
  for tool in "${ADAPTER_TOOLS[@]}"; do
    check_present "scenarios/${scenario}/adapters/${tool}.json"
  done
done

if has_entry "manifest.json"; then
  MANIFEST="$(tar_cat "${ROOT}/manifest.json")"
  MODE="$(jq -r '.mode // "unknown"' <<<"$MANIFEST")"
  FIXTURES_VERSION="$(jq -r '.fixturesVersion // "missing"' <<<"$MANIFEST")"
  if [[ "$FIXTURES_VERSION" == "phase0-2026-05-02" ]]; then
    report PASS "fixturesVersion is phase0-2026-05-02"
  else
    report FAIL "fixturesVersion is phase0-2026-05-02" "$FIXTURES_VERSION"
  fi
  if [[ "$MODE" == "real-vps" || "$MODE" == "mock" ]]; then
    report PASS "mode is recognized" "$MODE"
  else
    report FAIL "mode is recognized" "$MODE"
  fi
fi

SECRET_PATTERNS=(
  'Authorization:[[:space:]]*(Bearer|Basic)[[:space:]]+[A-Za-z0-9._+/-]{8,}'
  'bearer[_-]?token["'\'':[:space:]=]+[A-Za-z0-9._+-]{12,}'
  'api[_-]?key["'\'':[:space:]=]+[A-Za-z0-9._+-]{12,}'
  'sk-[A-Za-z0-9_-]{20,}'
  'sk_(live|test)_[A-Za-z0-9]{16,}'
  'gh[pousr]_[A-Za-z0-9]{20,}'
  'glpat-[A-Za-z0-9_-]{20,}'
  'xox[baprs]-[A-Za-z0-9-]{12,}'
  'AKIA[A-Z0-9]{16}'
  '-----BEGIN [A-Z ]*PRIVATE KEY-----'
  'hp-(bearer|pairing|wstoken|pat|refresh)-[A-Za-z0-9._+-]+'
  '(hpat_|hopaq_|ya29\.|npm_|sntrys_)[A-Za-z0-9._+-]{8,}'
  'eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
)

SECRET_HITS="$(mktemp "${TMPDIR:-/tmp}/hoopoe-evidence-secret-hits.XXXXXX")"
: > "$SECRET_HITS"
while IFS= read -r entry; do
  case "$entry" in
    */|*.bin|*.png|*.jpg|*.jpeg|*.webp|*.zip|*.tar|*.zst) continue;;
  esac
  content="$(tar_cat "$entry" 2>/dev/null | head -c 5242880 || true)"
  for pattern in "${SECRET_PATTERNS[@]}"; do
    if grep -E -i -q -- "$pattern" <<<"$content"; then
      printf '%s :: %s\n' "$entry" "$pattern" >> "$SECRET_HITS"
    fi
  done
done < "$LIST_PATH"

if [[ -s "$SECRET_HITS" ]]; then
  report FAIL "secret scan clean" "$(head -n 5 "$SECRET_HITS" | tr '\n' '; ')"
else
  report PASS "secret scan clean"
fi

if [[ "$FAILURES" -eq 0 ]]; then
  printf 'PASS hp-vtwm evidence pack verifier (%s checks)\n' "$CHECKS"
  exit 0
fi

printf 'FAIL hp-vtwm evidence pack verifier (%s failure(s), %s checks)\n' "$FAILURES" "$CHECKS"
exit 1
