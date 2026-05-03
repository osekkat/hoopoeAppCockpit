#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[goldens] Regenerating Mock Flywheel scenario goldens"
HOOPOE_UPDATE_GOLDENS=1 bun test packages/fixtures/test/golden-replay.test.ts

echo "[goldens] Diff stat for regenerated artifacts"
git diff --stat -- 'packages/fixtures/scenarios/*/.goldens/**' || true

echo "[goldens] Git status for regenerated artifacts"
git status --short --untracked-files=all -- 'packages/fixtures/scenarios/*/.goldens/**'

echo "[goldens] Review these diffs before committing intentional golden updates:"
git diff -- 'packages/fixtures/scenarios/*/.goldens/**' || true
