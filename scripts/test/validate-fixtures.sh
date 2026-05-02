#!/usr/bin/env bash
# scripts/test/validate-fixtures.sh — fixture-quality CI gate (hp-pl5o).
#
# Runs the language-agnostic shell-side checks first (existence + JSON
# parseability + secret-pattern grep), then delegates to the bun-test
# suite for the fuller schema + shape assertions.
#
# Usage:
#   scripts/test/validate-fixtures.sh                     # default corpus root
#   scripts/test/validate-fixtures.sh --root <path>       # alternate corpus
#   scripts/test/validate-fixtures.sh --quick             # shell checks only
#
# Exit codes:
#   0  all checks pass
#   1  shell-side check failed (missing file / bad JSON / secret)
#   2  bun-test suite failed
#   3  rch / bun unavailable

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROOT="$REPO_ROOT/packages/fixtures"
QUICK=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root) ROOT="$2"; shift 2;;
    --quick) QUICK=1; shift;;
    -h|--help)
      sed -n '1,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | grep -E '^#( |$)' | sed 's/^# \{0,1\}//'
      exit 0;;
    *) echo "validate-fixtures.sh: unknown flag: $1" >&2; exit 2;;
  esac
done

if ! command -v jq >/dev/null 2>&1; then
  echo "validate-fixtures.sh: jq required" >&2
  exit 3
fi

ERR=0

# 1. Every JSON parses.
echo "validate-fixtures.sh: parse-check JSON files under $ROOT" >&2
while IFS= read -r f; do
  if ! jq . "$f" >/dev/null 2>&1; then
    echo "  [bad-json] $f" >&2
    ERR=$((ERR + 1))
  fi
done < <(find "$ROOT" -name '*.json' -type f -not -path '*/node_modules/*' -not -path '*/.turbo/*')

# 2. Every NDJSON parses.
echo "validate-fixtures.sh: parse-check NDJSON (events.jsonl)" >&2
while IFS= read -r f; do
  if ! while IFS= read -r line; do printf '%s\n' "$line" | jq . >/dev/null 2>&1 || exit 1; done < "$f"; then
    echo "  [bad-ndjson] $f" >&2
    ERR=$((ERR + 1))
  fi
done < <(find "$ROOT" -name 'events.jsonl' -type f -not -path '*/node_modules/*')

# 3. No provider-secret patterns. Guardrail 11.
#    Only assignment-context env vars + actual live-key shapes — bare env-var
#    NAMES in policy text are fine (and unavoidable in bead descriptions).
echo "validate-fixtures.sh: secret-pattern grep (Guardrail 11)" >&2
SECRET_PATTERNS='OPENAI_API_KEY[[:space:]]*[:=][[:space:]]*"?[A-Za-z0-9_-]{8,}|ANTHROPIC_API_KEY[[:space:]]*[:=][[:space:]]*"?[A-Za-z0-9_-]{8,}|GEMINI_API_KEY[[:space:]]*[:=][[:space:]]*"?[A-Za-z0-9_-]{8,}|\bsk-[A-Za-z0-9_-]{20,}|\bghp_[A-Za-z0-9]{36,}|\bAKIA[A-Z0-9]{16}\b|-----BEGIN [A-Z ]*PRIVATE KEY-----'
SCAN_DIRS=("$ROOT/scenarios" "$ROOT/golden-outputs")
for d in "$ROOT"/phase0-*; do
  [[ -d "$d" ]] && SCAN_DIRS+=("$d")
done
for sd in "${SCAN_DIRS[@]}"; do
  [[ -d "$sd" ]] || continue
  if hits="$(grep -rE --include='*.json' --include='*.jsonl' --include='*.md' --include='*.txt' --exclude-dir=node_modules --exclude-dir=.turbo "$SECRET_PATTERNS" "$sd" 2>/dev/null)"; then
    if [[ -n "$hits" ]]; then
      echo "  [secret-found in $sd]" >&2
      echo "$hits" | head -10 >&2
      ERR=$((ERR + 1))
    fi
  fi
done

# 4. Required scenario file presence (lightweight; full check via bun test).
echo "validate-fixtures.sh: scenario file-presence check" >&2
REQUIRED_FILES=(meta.json bv-triage.json br-list.json ntm-snapshot.json agent-mail-dump.json reservations.json events.jsonl capabilities.json expected-outcome.json)
SCENARIOS_DIR="$ROOT/scenarios"
if [[ -d "$SCENARIOS_DIR" ]]; then
  for sd in "$SCENARIOS_DIR"/*/; do
    [[ -d "$sd" ]] || continue
    name="$(basename "$sd")"
    has_meta=0
    if [[ -f "$sd/meta.json" ]]; then has_meta=1; fi
    if [[ "$has_meta" == "0" ]]; then
      # Skip scenario stubs — directories without meta.json are placeholders.
      continue
    fi
    for f in "${REQUIRED_FILES[@]}"; do
      if [[ ! -f "$sd/$f" ]]; then
        echo "  [missing] scenarios/$name/$f" >&2
        ERR=$((ERR + 1))
      fi
    done
    for d in pane-logs build-logs; do
      if [[ ! -d "$sd/$d" ]]; then
        echo "  [missing-dir] scenarios/$name/$d/" >&2
        ERR=$((ERR + 1))
      fi
    done
  done
fi

if (( ERR > 0 )); then
  echo "validate-fixtures.sh: $ERR shell-side error(s)" >&2
  exit 1
fi

if [[ "$QUICK" == "1" ]]; then
  echo "validate-fixtures.sh: shell checks PASS (--quick; bun-test suite skipped)" >&2
  exit 0
fi

# 5. Delegate to bun-test suite for schema + shape + completeness assertions.
if ! command -v bun >/dev/null 2>&1; then
  echo "validate-fixtures.sh: bun required for full suite (use --quick to skip)" >&2
  exit 3
fi

echo "validate-fixtures.sh: handing off to bun-test suite (packages/fixtures/tests/)" >&2
if command -v rch >/dev/null 2>&1; then
  rch exec -- bun test "$REPO_ROOT/packages/fixtures/tests/fixture-quality.test.ts"
else
  bun test "$REPO_ROOT/packages/fixtures/tests/fixture-quality.test.ts"
fi
RC=$?
if [[ "$RC" -ne 0 ]]; then
  echo "validate-fixtures.sh: bun-test suite FAILED (rc=$RC)" >&2
  exit 2
fi

echo "validate-fixtures.sh: ALL CHECKS PASS" >&2
