#!/usr/bin/env bash
# scripts/research-spike/lib/envelope.sh
#
# Capture envelope helpers for the Hoopoe research-spike snapshot script.
# Sourced by snapshot.sh. Not intended to be executed directly.
#
# Public surface:
#   envelope::run_capture <label> <argv...>      → emits one InvocationCapture JSON object on FD 3
#   envelope::tool_open <slug>                   → starts a ToolCapture; returns FD numbers via env
#   envelope::tool_close <slug>                  → emits the ToolCapture JSON to FD 3
#   envelope::set_tool_meta <slug> <key> <value> → adds a top-level field to a ToolCapture
#   envelope::add_capability <slug> <capId> <status> [fallback] [transport] [notes]
#   envelope::redact <text>                       → echoes the text with REDACT_PATTERNS substituted
#
# All output goes to FD 3 as newline-delimited JSON. snapshot.sh assembles the
# final document by streaming FD 3 through jq.
#
# Defaults:
#   ENVELOPE_MAX_BYTES            1048576     # 1 MiB per stream cap
#   ENVELOPE_TIMEOUT_S            30          # per-invocation timeout
#   ENVELOPE_REDACT               1           # 0 disables redaction
#
# Exit codes:
#   0  = capture completed (the wrapped command may itself have failed; that is
#        recorded inside the envelope, not propagated).
#   2  = envelope misuse (bad argv, missing FD 3, jq not on PATH).

set -o pipefail

: "${ENVELOPE_MAX_BYTES:=1048576}"
: "${ENVELOPE_TIMEOUT_S:=30}"
: "${ENVELOPE_REDACT:=1}"

# State for the currently-open ToolCapture. We support exactly one at a time —
# captures are sequential by design (snapshots must be deterministic; parallelism
# is the caller's concern).
__ENVELOPE_TOOL_SLUG=""
__ENVELOPE_TOOL_DIR=""

# Patterns we redact from stdout/stderr before storing. These match the audit-log
# redaction layer described in plan.md §5.1 / §10.2.
__ENVELOPE_REDACT_PATTERNS=(
  's/(Authorization:\s*Bearer\s+)[A-Za-z0-9._+-]+/\1<REDACTED>/gi'
  's/(bearer[_-]?token["'"'"':\s=]+)[A-Za-z0-9._+-]+/\1<REDACTED>/gi'
  's/(api[_-]?key["'"'"':\s=]+)[A-Za-z0-9._+-]+/\1<REDACTED>/gi'
  's/(sk-[A-Za-z0-9_-]{20,})/<REDACTED-OPENAI-KEY>/g'
  's/(ghp_[A-Za-z0-9]{36,})/<REDACTED-GITHUB-PAT>/g'
  's/(xox[baprs]-[A-Za-z0-9-]+)/<REDACTED-SLACK>/g'
  's/(AKIA[A-Z0-9]{16})/<REDACTED-AWS-ACCESS-KEY>/g'
  's/-----BEGIN [A-Z ]+PRIVATE KEY-----[^-]+-----END [A-Z ]+PRIVATE KEY-----/<REDACTED-PRIVATE-KEY-BLOCK>/g'
  's/(passphrase["'"'"':\s=]+)[^"'"'"',\s]+/\1<REDACTED>/gi'
  's/(password["'"'"':\s=]+)[^"'"'"',\s]+/\1<REDACTED>/gi'
)

envelope::__require() {
  command -v jq >/dev/null 2>&1 || {
    echo "envelope.sh: jq is required but not on PATH" >&2
    return 2
  }
  if ! { true >&3; } 2>/dev/null; then
    echo "envelope.sh: FD 3 must be opened by the caller (snapshot.sh)" >&2
    return 2
  fi
}

envelope::redact() {
  if [[ "${ENVELOPE_REDACT}" != "1" ]]; then
    cat
    return 0
  fi
  local sed_args=()
  local p
  for p in "${__ENVELOPE_REDACT_PATTERNS[@]}"; do
    sed_args+=("-e" "$p")
  done
  sed -E "${sed_args[@]}"
}

envelope::__now_iso() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

envelope::__elapsed_ms() {
  # $1 = start ns, $2 = end ns
  local start_ns=$1
  local end_ns=$2
  echo $(( (end_ns - start_ns) / 1000000 ))
}

# envelope::run_capture <label> -- <argv...>
#
# Run argv with stdin (if any) on FD 0, capture stdout/stderr/exit/duration,
# emit one InvocationCapture JSON object on FD 4 (per-tool sink). The currently
# open ToolCapture must collect these via FD 4 (set by tool_open).
#
# Optional env:
#   STDIN_PAYLOAD   if set, piped to the command's stdin
#   TIMEOUT_S       per-invocation timeout override
#   TAGS            comma-separated tag list
envelope::run_capture() {
  envelope::__require || return 2
  local label="$1"; shift
  if [[ "$1" == "--" ]]; then shift; fi
  if [[ $# -lt 1 ]]; then
    echo "envelope.sh: run_capture needs at least one argv element" >&2
    return 2
  fi
  local timeout_s="${TIMEOUT_S:-$ENVELOPE_TIMEOUT_S}"
  local tags="${TAGS:-}"

  if [[ -z "${__ENVELOPE_TOOL_DIR}" ]]; then
    echo "envelope.sh: run_capture called outside tool_open/close" >&2
    return 2
  fi

  local tmp_stdout tmp_stderr tmp_argv
  tmp_stdout="$(mktemp "${__ENVELOPE_TOOL_DIR}/stdout.XXXXXX")"
  tmp_stderr="$(mktemp "${__ENVELOPE_TOOL_DIR}/stderr.XXXXXX")"
  tmp_argv="$(mktemp "${__ENVELOPE_TOOL_DIR}/argv.XXXXXX")"
  local i
  for i in "$@"; do printf '%s\n' "$i"; done > "$tmp_argv"

  local start_ns end_ns
  start_ns="$(date +%s%N)"
  local exit_code=0
  if [[ -n "${STDIN_PAYLOAD:-}" ]]; then
    printf '%s' "$STDIN_PAYLOAD" \
      | timeout --preserve-status "${timeout_s}s" "$@" \
        >"$tmp_stdout" 2>"$tmp_stderr" \
      || exit_code=$?
  else
    timeout --preserve-status "${timeout_s}s" "$@" \
      </dev/null \
      >"$tmp_stdout" 2>"$tmp_stderr" \
      || exit_code=$?
  fi
  end_ns="$(date +%s%N)"

  # Truncation check + redaction.
  local truncated=false
  local stdout_bytes stderr_bytes
  stdout_bytes=$(wc -c <"$tmp_stdout" | tr -d ' ')
  stderr_bytes=$(wc -c <"$tmp_stderr" | tr -d ' ')
  if (( stdout_bytes > ENVELOPE_MAX_BYTES )); then
    head -c "$ENVELOPE_MAX_BYTES" "$tmp_stdout" > "$tmp_stdout.cut"
    mv "$tmp_stdout.cut" "$tmp_stdout"
    truncated=true
  fi
  if (( stderr_bytes > ENVELOPE_MAX_BYTES )); then
    head -c "$ENVELOPE_MAX_BYTES" "$tmp_stderr" > "$tmp_stderr.cut"
    mv "$tmp_stderr.cut" "$tmp_stderr"
    truncated=true
  fi

  local redacted=false
  if [[ "$ENVELOPE_REDACT" == "1" ]]; then
    local before_stdout before_stderr
    before_stdout="$(cat "$tmp_stdout")"
    before_stderr="$(cat "$tmp_stderr")"
    envelope::redact <"$tmp_stdout" >"$tmp_stdout.r" && mv "$tmp_stdout.r" "$tmp_stdout"
    envelope::redact <"$tmp_stderr" >"$tmp_stderr.r" && mv "$tmp_stderr.r" "$tmp_stderr"
    if [[ "$before_stdout" != "$(cat "$tmp_stdout")" ]] \
       || [[ "$before_stderr" != "$(cat "$tmp_stderr")" ]]; then
      redacted=true
    fi
  fi

  local duration_ms
  duration_ms="$(envelope::__elapsed_ms "$start_ns" "$end_ns")"

  # Try to parse stdout as JSON (or NDJSON: collect into an array).
  # Write the parsed JSON (if any) to a sidecar file so we can pass it via
  # --slurpfile and avoid ARG_MAX limits on big payloads (br schema, ntm
  # snapshot, etc.).
  local stdout_json_path=""
  if [[ "$truncated" == "false" ]] && [[ "$stdout_bytes" -gt 0 ]]; then
    if jq -e . <"$tmp_stdout" >/dev/null 2>&1; then
      stdout_json_path="${tmp_stdout}.json"
      jq -c . <"$tmp_stdout" > "$stdout_json_path"
    elif jq -e -cR '. as $l | try (fromjson | true) catch false' <"$tmp_stdout" 2>/dev/null \
           | jq -es 'length > 0 and all(.; .)' >/dev/null 2>&1; then
      stdout_json_path="${tmp_stdout}.json"
      jq -cRn '[inputs | fromjson]' <"$tmp_stdout" > "$stdout_json_path"
    fi
  fi

  local tags_json="[]"
  if [[ -n "$tags" ]]; then
    tags_json="$(jq -cn --arg s "$tags" '$s | split(",")')"
  fi

  local argv_json
  argv_json="$(jq -Rsc 'split("\n")[:-1]' <"$tmp_argv")"

  # Use --rawfile for stdout/stderr text and --slurpfile for parsed JSON to
  # bypass ARG_MAX on large payloads.
  local invocation
  if [[ -n "$stdout_json_path" ]]; then
    invocation="$(jq -cn \
      --arg label "$label" \
      --argjson argv "$argv_json" \
      --argjson exit "$exit_code" \
      --argjson dur "$duration_ms" \
      --argjson sob "$stdout_bytes" \
      --argjson seb "$stderr_bytes" \
      --slurpfile sjson "$stdout_json_path" \
      --rawfile stext "$tmp_stdout" \
      --rawfile etext "$tmp_stderr" \
      --argjson trunc "$truncated" \
      --argjson red "$redacted" \
      --argjson tags "$tags_json" \
      '{
        label: $label, argv: $argv, exit: $exit, durationMs: $dur,
        stdoutBytes: $sob, stderrBytes: $seb,
        stdoutJson: $sjson[0],
        stdoutText: $stext, stderrText: $etext,
        truncated: $trunc, redacted: $red, tags: $tags
      }')"
  else
    invocation="$(jq -cn \
      --arg label "$label" \
      --argjson argv "$argv_json" \
      --argjson exit "$exit_code" \
      --argjson dur "$duration_ms" \
      --argjson sob "$stdout_bytes" \
      --argjson seb "$stderr_bytes" \
      --rawfile stext "$tmp_stdout" \
      --rawfile etext "$tmp_stderr" \
      --argjson trunc "$truncated" \
      --argjson red "$redacted" \
      --argjson tags "$tags_json" \
      '{
        label: $label, argv: $argv, exit: $exit, durationMs: $dur,
        stdoutBytes: $sob, stderrBytes: $seb,
        stdoutText: $stext, stderrText: $etext,
        truncated: $trunc, redacted: $red, tags: $tags
      }')"
  fi

  printf '%s\n' "$invocation" >&4
  rm -f "$tmp_stdout" "$tmp_stderr" "$tmp_argv" "${stdout_json_path:-}"
}

# envelope::tool_open <slug>
#
# Begins a ToolCapture. Resolves binPath + version automatically when possible
# (calls `<bin> --version` with a 5s timeout; failures are non-fatal). Opens
# FD 4 backed by a tempfile that collects per-invocation JSON.
#
# Tool_open is reentrant per-process via __ENVELOPE_TOOL_SLUG; nested opens
# error out.
envelope::tool_open() {
  envelope::__require || return 2
  if [[ -n "${__ENVELOPE_TOOL_SLUG}" ]]; then
    echo "envelope.sh: tool_open called while '${__ENVELOPE_TOOL_SLUG}' is still open" >&2
    return 2
  fi
  local slug="$1"
  __ENVELOPE_TOOL_SLUG="$slug"
  __ENVELOPE_TOOL_DIR="$(mktemp -d "${TMPDIR:-/tmp}/hoopoe-snapshot.${slug}.XXXXXX")"
  exec 4>"${__ENVELOPE_TOOL_DIR}/captures.ndjson"
  : > "${__ENVELOPE_TOOL_DIR}/meta.json"
  : > "${__ENVELOPE_TOOL_DIR}/capabilities.ndjson"
  : > "${__ENVELOPE_TOOL_DIR}/errors.ndjson"
  printf '{"present": false}\n' > "${__ENVELOPE_TOOL_DIR}/meta.json"
}

envelope::set_tool_meta() {
  local slug="$1"; local key="$2"; local value="$3"
  if [[ "$slug" != "$__ENVELOPE_TOOL_SLUG" ]]; then
    echo "envelope.sh: set_tool_meta '$slug' but open slug is '$__ENVELOPE_TOOL_SLUG'" >&2
    return 2
  fi
  local current
  current="$(cat "${__ENVELOPE_TOOL_DIR}/meta.json")"
  printf '%s' "$current" \
    | jq --arg k "$key" --arg v "$value" '. + {($k): $v}' \
    > "${__ENVELOPE_TOOL_DIR}/meta.json.tmp"
  mv "${__ENVELOPE_TOOL_DIR}/meta.json.tmp" "${__ENVELOPE_TOOL_DIR}/meta.json"
}

envelope::set_tool_meta_bool() {
  local slug="$1"; local key="$2"; local value="$3"
  if [[ "$slug" != "$__ENVELOPE_TOOL_SLUG" ]]; then
    echo "envelope.sh: set_tool_meta_bool '$slug' but open slug is '$__ENVELOPE_TOOL_SLUG'" >&2
    return 2
  fi
  local current
  current="$(cat "${__ENVELOPE_TOOL_DIR}/meta.json")"
  printf '%s' "$current" \
    | jq --arg k "$key" --argjson v "$value" '. + {($k): $v}' \
    > "${__ENVELOPE_TOOL_DIR}/meta.json.tmp"
  mv "${__ENVELOPE_TOOL_DIR}/meta.json.tmp" "${__ENVELOPE_TOOL_DIR}/meta.json"
}

envelope::add_capability() {
  local slug="$1"; local cap="$2"; local status="$3"
  local fallback="${4:-}"; local transport="${5:-}"; local notes="${6:-}"
  if [[ "$slug" != "$__ENVELOPE_TOOL_SLUG" ]]; then
    echo "envelope.sh: add_capability '$slug' but open slug is '$__ENVELOPE_TOOL_SLUG'" >&2
    return 2
  fi
  jq -cn \
    --arg cap "$cap" \
    --arg status "$status" \
    --arg fallback "$fallback" \
    --arg transport "$transport" \
    --arg notes "$notes" \
    '{cap: $cap, value: ({status: $status}
       + (if $fallback == "" then {} else {fallback: $fallback} end)
       + (if $transport == "" then {} else {transport: $transport} end)
       + (if $notes == "" then {} else {notes: $notes} end))}' \
    >> "${__ENVELOPE_TOOL_DIR}/capabilities.ndjson"
}

envelope::record_error() {
  local slug="$1"; local message="$2"
  if [[ "$slug" != "$__ENVELOPE_TOOL_SLUG" ]]; then
    echo "envelope.sh: record_error '$slug' but open slug is '$__ENVELOPE_TOOL_SLUG'" >&2
    return 2
  fi
  jq -cn --arg m "$message" '{m: $m}' >> "${__ENVELOPE_TOOL_DIR}/errors.ndjson"
}

# envelope::probe_version <slug> <bin>
#
# Convenience: tries `<bin> --version`, then `<bin> -V`, then `<bin> version`.
# Sets binPath, version, present.
envelope::probe_version() {
  local slug="$1"; local bin="$2"
  local path
  if path="$(command -v "$bin" 2>/dev/null)"; then
    envelope::set_tool_meta "$slug" binPath "$path"
    envelope::set_tool_meta_bool "$slug" present true
  else
    envelope::set_tool_meta_bool "$slug" present false
    envelope::set_tool_meta "$slug" skipReason "missing binary on PATH: $bin"
    return 1
  fi
  local v=""
  v="$(timeout 5 "$bin" --version 2>/dev/null | head -n 1)" || true
  if [[ -z "$v" ]]; then
    v="$(timeout 5 "$bin" -V 2>/dev/null | head -n 1)" || true
  fi
  if [[ -z "$v" ]]; then
    v="$(timeout 5 "$bin" version 2>/dev/null | head -n 1)" || true
  fi
  if [[ -n "$v" ]]; then
    envelope::set_tool_meta "$slug" version "$v"
  fi
  return 0
}

envelope::tool_close() {
  local slug="$1"
  if [[ "$slug" != "$__ENVELOPE_TOOL_SLUG" ]]; then
    echo "envelope.sh: tool_close '$slug' but open slug is '$__ENVELOPE_TOOL_SLUG'" >&2
    return 2
  fi
  exec 4>&-
  local meta_path="${__ENVELOPE_TOOL_DIR}/meta.json"
  local caps_path="${__ENVELOPE_TOOL_DIR}/capabilities.json"
  local invs_path="${__ENVELOPE_TOOL_DIR}/captures.json"
  local errs_path="${__ENVELOPE_TOOL_DIR}/errors.json"

  if [[ -s "${__ENVELOPE_TOOL_DIR}/capabilities.ndjson" ]]; then
    jq -cs 'map({(.cap): .value}) | add' \
       "${__ENVELOPE_TOOL_DIR}/capabilities.ndjson" > "$caps_path"
  else
    printf '{}' > "$caps_path"
  fi

  if [[ -s "${__ENVELOPE_TOOL_DIR}/captures.ndjson" ]]; then
    jq -cs 'map({(.label): (del(.label))}) | add' \
       "${__ENVELOPE_TOOL_DIR}/captures.ndjson" > "$invs_path"
  else
    printf '{}' > "$invs_path"
  fi

  if [[ -s "${__ENVELOPE_TOOL_DIR}/errors.ndjson" ]]; then
    jq -cs 'map(.m)' "${__ENVELOPE_TOOL_DIR}/errors.ndjson" > "$errs_path"
  else
    printf '[]' > "$errs_path"
  fi

  local now_iso
  now_iso="$(envelope::__now_iso)"
  jq -c \
    --arg slug "$slug" \
    --slurpfile caps "$caps_path" \
    --slurpfile invs "$invs_path" \
    --slurpfile errs "$errs_path" \
    --arg now "$now_iso" \
    '{tool: $slug}
     + .
     + (if (($caps[0]) | length) == 0 then {} else {capabilities: $caps[0]} end)
     + {captures: $invs[0]}
     + (if (($errs[0]) | length) == 0 then {} else {errors: $errs[0]} end)
     + {capturedAt: $now}' \
    "$meta_path" >&3

  rm -rf "${__ENVELOPE_TOOL_DIR}"
  __ENVELOPE_TOOL_SLUG=""
  __ENVELOPE_TOOL_DIR=""
}
