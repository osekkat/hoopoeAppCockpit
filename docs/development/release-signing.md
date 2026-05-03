# Hoopoe Desktop Release Signing

This document records the Phase 1.5 packaging path for `hp-2ae3`.

## Local Acceptance

The local host does not have Apple Developer ID material, so local acceptance uses
a deterministic mock artifact and a test signing key:

```bash
rch exec -- bun run --cwd apps/desktop build
rch exec -- bun scripts/build-desktop-artifact.ts \
  --platform mac \
  --target dmg \
  --arch arm64 \
  --build-version 0.0.0-hp-2ae3.1 \
  --output-dir /tmp/hoopoe-release-acceptance \
  --mock-updates \
  --mock-artifact
```

`--mock-artifact` writes:

- `Hoopoe-<version>-<arch>.dmg` - a DMG-shaped acceptance artifact, not a real disk image.
- `latest-mac.yml` - electron-updater generic-provider metadata with SHA-512.
- `update.json` - Hoopoe's test update manifest with SHA-512, a stub notarization result, and an Ed25519 test signature.

The release test starts `scripts/mock-update-server.ts`, fetches `update.json`,
verifies the signature and checksum, then uses electron-updater's generic
provider parser against `latest-mac.yml`.

```bash
rch exec -- bun test apps/desktop/tests/release/dmg-build.test.ts
rch exec -- bun run test:desktop:release
```

## Real Signed Release

Real releases run only on `macos-14` through `.github/workflows/release.yml`.
They must not fall back to unsigned publication. The workflow passes `--signed`
only when every required secret is present; otherwise it fails before artifact
upload.

Required GitHub Actions secrets:

| Secret | Purpose |
| --- | --- |
| `CSC_LINK` | Base64 encoded Developer ID Application certificate, or an electron-builder-supported certificate link. |
| `CSC_KEY_PASSWORD` | Password for `CSC_LINK`. |
| `APPLE_API_KEY` | App Store Connect API private key contents. The workflow writes it to `RUNNER_TEMP/AuthKey_<id>.p8`. |
| `APPLE_API_KEY_ID` | App Store Connect API key id. |
| `APPLE_API_ISSUER` | App Store Connect issuer UUID. |
| `GH_TOKEN` | Optional release token. If absent, `github.token` is used. |

Real release command inside CI:

```bash
bun scripts/build-desktop-artifact.ts \
  --platform mac \
  --target dmg \
  --arch arm64 \
  --build-version "$VERSION" \
  --signed \
  --verbose
```

For the x64 matrix entry, `--arch x64` is used. The build script configures
Electron Builder's hardened runtime and notarization block when `--signed` is
enabled.

## What Changes From Mock To Real

| Path | Mock acceptance | Real release |
| --- | --- | --- |
| Artifact build | `--mock-artifact`, no Apple material, deterministic DMG-shaped file. | Electron Builder produces a real DMG and ZIP on `macos-14`. |
| Signing | Ed25519 test signature over `update.json` payload. | Developer ID Application signing via `CSC_LINK` and `CSC_KEY_PASSWORD`. |
| Notarization | `update.json.notarization.mode = "stub"`. | App Store Connect notarization via `APPLE_API_KEY`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER`. |
| Update metadata | `latest-mac.yml` served by local `mock-update-server`. | `latest-mac.yml` / release assets uploaded to GitHub Releases. |
| Publication | Local only; never uploaded. | `softprops/action-gh-release` publishes only after preflight and both signed build matrix jobs pass. |

## Evidence From This Pass

Commands run on 2026-05-03:

- `rch exec -- bun run --cwd apps/desktop build` passed.
- `rch exec -- bun scripts/build-desktop-artifact.ts --platform mac --target dmg --arch x64 --mock-updates --mock-artifact --skip-build --build-version 0.0.0-hp-2ae3.1 --output-dir /tmp/hoopoe-hp-2ae3-release-mock --verbose` passed and produced a DMG-shaped artifact plus `latest-mac.yml` and `update.json`.
- `rch exec -- bun scripts/build-desktop-artifact.ts --platform mac --target dmg --arch x64 --mock-updates --skip-build --build-version 0.0.0-hp-2ae3.1 --output-dir /tmp/hoopoe-hp-2ae3-real-dmg-attempt --verbose` reached Electron Builder but failed on this Linux host with `Cannot find package 'dmg-license' ... dmg-builder`. Real DMG creation remains a macOS CI responsibility.
- `rch exec -- bun test apps/desktop/tests/release/dmg-build.test.ts` passed.

No real Apple signing or notarization was attempted in this pass.
