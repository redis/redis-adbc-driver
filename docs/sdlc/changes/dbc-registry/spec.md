# Specification: dbc packages and catalog

Date: 2026-10-29. Source: `95d6583`. Requestor: repository user.
Status: implemented locally; public registry publication pending.

Produce flat gzip tar archives containing MANIFEST, the native library and
LICENSE for linux_amd64, linux_arm64 and macos_arm64. Declare ADBC 1.1.0 and
AdbcDriverRedisInit. Generate checksums and a JSON-compatible YAML index using
only built assets, with official redis/redis-adbc-driver release URLs. Preserve
existing distributions. Unsigned local installation requires --no-verify.

The checked-in v0.0.18 catalog is a proposal: its additional assets have not
been uploaded. Default-registry installation requires maintainer onboarding,
asset publication and trusted signatures. No release or external message is
authorized or performed by these local changes.

See [intent](intent.md) and [plan](plan.md).
