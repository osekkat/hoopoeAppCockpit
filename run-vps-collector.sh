#!/usr/bin/env bash
# Wrapper around scripts/research-spike/collect-evidence-pack.sh
# Hardcoded to the user's research-spike VPS at 45.85.250.216 (admin@).
# Re-run this whenever you change something on the VPS or in the collector.

set -euo pipefail

REPO_ROOT="/home/ubuntu/Projects/hoopoeAppCockpit"
VPS="admin@45.85.250.216"
PROJECT_DIR="/home/admin/Projects/nexusAudio"
VPS_ID="research-spike-2026-05-04"
FIXTURES_VERSION="phase0-2026-05-02"
LANDING_DIR="packages/fixtures/phase0-2026-05-02/incoming"
WIZARD_NOTES='echo "manual wizard pending — see hp-jvm follow-up"'

# Optional: pin ACFS tag (set ACFS_TAG=v0.42.1 to override)
ACFS_TAG="${ACFS_TAG:-}"
ACFS_TAG_ARG=()
if [[ -n "${ACFS_TAG}" ]]; then
  ACFS_TAG_ARG=(--acfs-tag "${ACFS_TAG}")
fi

cd "${REPO_ROOT}"

echo "=== pre-flight: SSH key auth + project + tools ==="
ssh -o BatchMode=yes "${VPS}" '
  set -e
  whoami
  pwd
  command -v acfs >/dev/null && echo "OK  acfs" || echo "??  acfs (will reduce capture coverage)"
  test -d '"${PROJECT_DIR}"'/.git && echo "OK  '"${PROJECT_DIR}"' is a git repo" || { echo "FAIL  '"${PROJECT_DIR}"' is not a git repo"; exit 1; }
'

echo ""
echo "=== running collector ==="
scripts/research-spike/collect-evidence-pack.sh \
  --ssh "${VPS}" \
  --project-dir "${PROJECT_DIR}" \
  --vps-id "${VPS_ID}" \
  "${ACFS_TAG_ARG[@]}" \
  --fixtures-version "${FIXTURES_VERSION}" \
  --wizard-command "${WIZARD_NOTES}" \
  --landing-dir "${LANDING_DIR}"

echo ""
echo "=== result ==="
ls -la "${LANDING_DIR}/" 2>/dev/null | tail -10
