#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: bootstrap.sh --daemon-url URL --manifest-url URL --signature-url URL --attestation-url URL --sbom-url URL --trusted-key PATH [options]

Installs or upgrades the Hoopoe daemon on an existing VPS, creates the first
pairing token when needed, installs the user systemd unit, and starts the
service.

Options:
  --daemon-url URL                 signed release binary URL
  --daemon-sha256 SHA256           expected daemon binary SHA256
  --binary-path PATH               local daemon binary path instead of URL
  --manifest-url URL               release manifest URL
  --signature-url URL              Ed25519 signature URL
  --attestation-url URL            provenance attestation URL
  --sbom-url URL                   daemon SBOM URL
  --cve-db-url URL                 last-known-good CVE database URL
  --trusted-key PATH               trusted release key JSON
  --release-verify-cmd COMMAND     release verifier command
  --addr HOST:PORT                 daemon listen address, default 127.0.0.1:8756
  --acfs-install-url URL           ACFS one-line installer URL when tools are missing
  --acfs-install-sha256 SHA256     expected ACFS installer SHA256
  --skip-acfs                     skip ACFS tool preflight/install
  --insecure-dev-override          allow install without release verifier; not for production
  --override-actor ACTOR           actor stamped on insecure override audit
  --override-reason REASON         required reason when override is used
  --sbom-ack                       acknowledge blocking SBOM findings
  --sbom-ack-actor ACTOR           actor stamped on SBOM acknowledgement audit
  --sbom-ack-reason REASON         required reason when SBOM acknowledgement is used
  --help                           show this help
USAGE
}

die() {
  printf 'bootstrap: %s\n' "$*" >&2
  exit 1
}

log() {
  printf 'bootstrap: %s\n' "$*" >&2
}

json_escape() {
  local value=${1//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  printf '%s' "$value"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

verify_sha256() {
  local path=$1
  local expected=$2
  [ -n "$expected" ] || return 0
  local actual
  actual=$(sha256_file "$path")
  expected=${expected#sha256:}
  [ "$actual" = "$expected" ] || die "checksum mismatch for $path: got $actual want $expected"
}

download_file() {
  local url=$1
  local path=$2
  [ -n "$url" ] || die "download URL missing for $path"
  curl -fsSL --retry 3 --retry-delay 1 "$url" -o "$path"
}

write_audit() {
  local event=$1
  local result=$2
  local detail=$3
  local ts
  ts=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  mkdir -p "$HOOPOE_HOME"
  printf '{"schemaVersion":1,"source":"bootstrap.sh","event":"%s","result":"%s","time":"%s","detail":"%s"}\n' \
    "$(json_escape "$event")" "$(json_escape "$result")" "$ts" "$(json_escape "$detail")" >>"$HOOPOE_HOME/audit.jsonl"
}

install_acfs_if_needed() {
  [ "${HOOPOE_SKIP_ACFS:-0}" = "1" ] && return 0
  if command -v br >/dev/null 2>&1 && command -v bv >/dev/null 2>&1 && command -v ntm >/dev/null 2>&1; then
    log "ACFS toolchain already present"
    return 0
  fi
  [ -n "${HOOPOE_ACFS_INSTALL_URL:-}" ] || die "ACFS tools missing; pass --acfs-install-url or --skip-acfs"
  local installer="$HOOPOE_RELEASE_DIR/acfs-install.sh"
  download_file "$HOOPOE_ACFS_INSTALL_URL" "$installer"
  verify_sha256 "$installer" "${HOOPOE_ACFS_INSTALL_SHA256:-}"
  bash "$installer"
}

resolve_default_verifier() {
  if [ -n "${HOOPOE_RELEASE_VERIFY_CMD:-}" ]; then
    printf '%s' "$HOOPOE_RELEASE_VERIFY_CMD"
    return 0
  fi
  if [ -f "$SCRIPT_DIR/verify-release/main.go" ] && command -v go >/dev/null 2>&1; then
    printf '%s' "go run ./scripts/install/verify-release"
    return 0
  fi
  return 1
}

verify_release() {
  verify_sha256 "$HOOPOE_STAGED_BINARY" "${HOOPOE_DAEMON_SHA256:-}"
  local verifier=""
  local override_args=()
  local sbom_args=()
  if [ "${HOOPOE_INSECURE_DEV_OVERRIDE:-0}" = "1" ]; then
    HOOPOE_OVERRIDE_REASON=${HOOPOE_OVERRIDE_REASON:-"operator requested bootstrap dev override"}
    override_args=(-override -override-actor "${HOOPOE_OVERRIDE_ACTOR:-operator}" -override-reason "$HOOPOE_OVERRIDE_REASON")
  fi
  if [ "${HOOPOE_SBOM_ACK:-0}" = "1" ]; then
    HOOPOE_SBOM_ACK_REASON=${HOOPOE_SBOM_ACK_REASON:-"operator acknowledged bootstrap SBOM policy"}
    sbom_args=(-sbom-ack -sbom-ack-actor "${HOOPOE_SBOM_ACK_ACTOR:-operator}" -sbom-ack-reason "$HOOPOE_SBOM_ACK_REASON")
  fi
  if verifier=$(resolve_default_verifier); then
    (
      cd "$DAEMON_ROOT"
      $verifier \
        -binary "$HOOPOE_STAGED_BINARY" \
        -manifest "$HOOPOE_RELEASE_DIR/manifest.json" \
        -signature "$HOOPOE_RELEASE_DIR/hoopoed.sig" \
        -attestation "$HOOPOE_RELEASE_DIR/attestation.json" \
        -sbom "$HOOPOE_RELEASE_DIR/sbom.json" \
        -cve-db "$HOOPOE_RELEASE_DIR/cve-db.json" \
        -trusted-key "$HOOPOE_TRUSTED_KEY" \
        -inventory "$HOOPOE_RELEASE_DIR/inventory.json" \
        -audit "$HOOPOE_HOME/audit.jsonl" \
        "${override_args[@]}" \
        "${sbom_args[@]}"
    )
    return 0
  fi
  [ "${HOOPOE_INSECURE_DEV_OVERRIDE:-0}" = "1" ] || die "release verifier unavailable; pass --release-verify-cmd or --insecure-dev-override"
  write_audit "daemon.release.verification_override" "override" "operator supplied insecure dev override"
}

write_systemd_unit() {
  local source_unit="$DAEMON_ROOT/systemd/hoopoe.service"
  if [ -f "$source_unit" ]; then
    install -m 0644 "$source_unit" "$HOOPOE_UNIT_PATH"
    return 0
  fi
  cat >"$HOOPOE_UNIT_PATH" <<'UNIT'
[Unit]
Description=Hoopoe VPS daemon
Wants=network-online.target
After=network-online.target

[Service]
Type=notify
NotifyAccess=main
Environment=HOOPOE_DAEMON_ADDR=127.0.0.1:8756
ExecStart=%h/.hoopoe/bin/hoopoed -addr ${HOOPOE_DAEMON_ADDR} -state-dir %h/.hoopoe
Restart=on-failure
WatchdogSec=30
KillMode=mixed
TimeoutStopSec=20
LimitNOFILE=65536
ProtectSystem=strict
ReadWritePaths=%h/.hoopoe /data/projects /tmp
NoNewPrivileges=true
PrivateTmp=true
WorkingDirectory=%h

[Install]
WantedBy=default.target
UNIT
}

wait_for_health() {
  local url="http://${HOOPOE_DAEMON_ADDR}/health"
  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  journalctl --user-unit hoopoe.service -n 80 --no-pager >&2 || true
  die "daemon did not become healthy at $url"
}

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
DAEMON_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
HOOPOE_HOME=${HOOPOE_HOME:-"$HOME/.hoopoe"}
HOOPOE_BIN_DIR=${HOOPOE_BIN_DIR:-"$HOOPOE_HOME/bin"}
HOOPOE_RELEASE_DIR=${HOOPOE_RELEASE_DIR:-"$HOOPOE_HOME/release"}
HOOPOE_SYSTEMD_USER_DIR=${HOOPOE_SYSTEMD_USER_DIR:-"$HOME/.config/systemd/user"}
HOOPOE_UNIT_PATH=${HOOPOE_UNIT_PATH:-"$HOOPOE_SYSTEMD_USER_DIR/hoopoe.service"}
HOOPOE_DAEMON_ADDR=${HOOPOE_DAEMON_ADDR:-"127.0.0.1:8756"}
HOOPOE_TRUSTED_KEY=${HOOPOE_TRUSTED_KEY:-}
HOOPOE_STAGED_BINARY="$HOOPOE_RELEASE_DIR/hoopoed"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --daemon-url) HOOPOE_DAEMON_URL=$2; shift 2 ;;
    --daemon-sha256) HOOPOE_DAEMON_SHA256=$2; shift 2 ;;
    --binary-path) HOOPOE_DAEMON_BINARY_PATH=$2; shift 2 ;;
    --manifest-url) HOOPOE_MANIFEST_URL=$2; shift 2 ;;
    --signature-url) HOOPOE_SIGNATURE_URL=$2; shift 2 ;;
    --attestation-url) HOOPOE_ATTESTATION_URL=$2; shift 2 ;;
    --sbom-url) HOOPOE_SBOM_URL=$2; shift 2 ;;
    --cve-db-url) HOOPOE_CVE_DB_URL=$2; shift 2 ;;
    --trusted-key) HOOPOE_TRUSTED_KEY=$2; shift 2 ;;
    --release-verify-cmd) HOOPOE_RELEASE_VERIFY_CMD=$2; shift 2 ;;
    --addr) HOOPOE_DAEMON_ADDR=$2; shift 2 ;;
    --acfs-install-url) HOOPOE_ACFS_INSTALL_URL=$2; shift 2 ;;
    --acfs-install-sha256) HOOPOE_ACFS_INSTALL_SHA256=$2; shift 2 ;;
    --skip-acfs) HOOPOE_SKIP_ACFS=1; shift ;;
    --insecure-dev-override) HOOPOE_INSECURE_DEV_OVERRIDE=1; shift ;;
    --override-actor) HOOPOE_OVERRIDE_ACTOR=$2; shift 2 ;;
    --override-reason) HOOPOE_OVERRIDE_REASON=$2; shift 2 ;;
    --sbom-ack) HOOPOE_SBOM_ACK=1; shift ;;
    --sbom-ack-actor) HOOPOE_SBOM_ACK_ACTOR=$2; shift 2 ;;
    --sbom-ack-reason) HOOPOE_SBOM_ACK_REASON=$2; shift 2 ;;
    --help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

require_command awk
require_command bash
require_command chmod
require_command curl
require_command date
require_command install
require_command mkdir
require_command seq
require_command sha256sum
require_command systemctl

systemctl --user show-environment >/dev/null 2>&1 || die "systemd --user is unavailable in this SSH session"

mkdir -p "$HOOPOE_BIN_DIR" "$HOOPOE_RELEASE_DIR" "$HOOPOE_SYSTEMD_USER_DIR"
write_audit "daemon.bootstrap.started" "started" "bootstrap invoked"

install_acfs_if_needed

if [ -n "${HOOPOE_DAEMON_BINARY_PATH:-}" ]; then
  install -m 0755 "$HOOPOE_DAEMON_BINARY_PATH" "$HOOPOE_STAGED_BINARY"
else
  download_file "${HOOPOE_DAEMON_URL:-}" "$HOOPOE_STAGED_BINARY"
  chmod 0755 "$HOOPOE_STAGED_BINARY"
fi
download_file "${HOOPOE_MANIFEST_URL:-}" "$HOOPOE_RELEASE_DIR/manifest.json"
download_file "${HOOPOE_SIGNATURE_URL:-}" "$HOOPOE_RELEASE_DIR/hoopoed.sig"
download_file "${HOOPOE_ATTESTATION_URL:-}" "$HOOPOE_RELEASE_DIR/attestation.json"
download_file "${HOOPOE_SBOM_URL:-}" "$HOOPOE_RELEASE_DIR/sbom.json"
if [ -n "${HOOPOE_CVE_DB_URL:-}" ]; then
  download_file "$HOOPOE_CVE_DB_URL" "$HOOPOE_RELEASE_DIR/cve-db.json"
else
  printf '{"schemaVersion":1,"records":[]}\n' >"$HOOPOE_RELEASE_DIR/cve-db.json"
fi
[ -n "$HOOPOE_TRUSTED_KEY" ] || [ "${HOOPOE_INSECURE_DEV_OVERRIDE:-0}" = "1" ] || die "--trusted-key is required for release verification"

verify_release
install -m 0755 "$HOOPOE_STAGED_BINARY" "$HOOPOE_BIN_DIR/hoopoed"
write_audit "daemon.binary.installed" "ok" "$HOOPOE_BIN_DIR/hoopoed"

token_output=$("$HOOPOE_BIN_DIR/hoopoed" -state-dir "$HOOPOE_HOME" -bootstrap-token-only)
write_systemd_unit
systemctl --user daemon-reload
systemctl --user enable hoopoe.service >/dev/null
systemctl --user restart hoopoe.service
wait_for_health
write_audit "daemon.service.started" "ok" "$HOOPOE_DAEMON_ADDR"

printf '%s\n' "$token_output"
printf 'HOOPOE_DAEMON_ADDR=%s\n' "$HOOPOE_DAEMON_ADDR"
