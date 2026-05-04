# Daemon Release Verification Contract

Bootstrap and upgrade flows must verify the daemon binary before install by
calling `apps/daemon/internal/release.Verifier`.

Required release bundle inputs:

- daemon binary bytes
- release manifest with artifact SHA-256, source commit, signing identity,
  builder identity, workflow path/ref, and minimum compatible versions
- detached Ed25519 signature over the verifier's release payload
- SLSA provenance attestation with matching subject digest
- SBOM plus the last-known-good CVE database generated during release

Install gates:

- checksum, signature, malformed SBOM, or provenance failure refuses install
- insecure development override can bypass those failures only when actor,
  reason, timestamp, and audit write all succeed
- high or critical SBOM findings require explicit user acknowledgement and are
  recorded in the daemon inventory
- successful verification writes `~/.hoopoe/daemon-inventory.json` so
  Diagnostics can show version, checksum, signing identity, SBOM digest,
  attestation digest, source commit, builder identity, and any warning banner
