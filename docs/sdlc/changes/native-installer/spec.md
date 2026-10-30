# Specification: managed native lifecycle

Date: 2026-10-29. Base: `b1d0613`. Inputs: [intent](intent.md).
Implementation: [plan](plan.md). Results: [validation](validation.md).

## Contracts

- A standalone Python 3.12+ standard-library script supports install, upgrade and
  uninstall. Detect Linux glibc 2.28+ amd64/arm64 or macOS 12+ arm64. Resolve latest
  stable or a pinned vMAJOR.MINOR.PATCH tag, verify the release SHA-256 and host
  binary format/architecture, then load-check AdbcDriverInit in a child process.
- Use HTTPS for every download/redirect; bound downloads and archive expansion.
  Extract only the native library and optional header. Reject traversal, links,
  duplicates, missing libraries and mismatched checksum filenames. Offline mode
  requires both archive/checksum files and a pinned version.
- Store files at a stable per-user prefix and register both driver names at the
  standard per-user manifest directory or an explicit custom location. Track
  file hashes, version and original manifests in private install-state.json.
- Refuse unmanaged library files, existing manifests without explicit replacement,
  moved/unknown state, modified managed files, target changes and downgrades.
  Back up opted-in existing manifests and their permission modes; preserve these
  originals through upgrades and restore them on uninstall.
- Serialize installer operations with a lock. Atomic per-file replacements and
  rollback on detected errors preserve the previous installation. Preserve backup
  files plus restore.json if rollback fails. Multi-file power-loss atomicity is
  explicitly outside this contract; document interrupted-process recovery.
- Uninstall removes only verified managed files, restores originals and retains
  unrelated data. Do not remove Python packages, Redis data or Redis servers.
- doctor accepts --ca-file and uses the same explicit TLS trust bundle for the
  direct connection and both discovery checks. Preserve native certificate and
  hostname verification and redact connection credentials.
- Live checks cover verified Software TLS with effective 4x QPF, authentication,
  Search ACL denial, untrusted CA, broken manifests and missing libraries. Use
  a separate negative endpoint without Search to verify that diagnostic.

## Limits

Checksums are supplied by the same GitHub release as the archives, not an
independent signature. Running clients must restart after upgrades. A Python
wheel's bundled driver and the managed native installation may have different
versions. Local native testing covers macOS arm64; CI covers Linux architectures.
