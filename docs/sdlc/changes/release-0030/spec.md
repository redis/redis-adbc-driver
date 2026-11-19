# Specification

Date: 2026-11-19. Status: preparation. PR: [#534](https://github.com/redis/redis-adbc-driver/pull/534).
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- Create a new immutable v0.0.30 tag and stable GitHub release from reviewed main.
  Never move existing tags or replace a previous release's assets.
- Replace obsolete live-feature source-build requirements with v0.0.30 availability.
- Describe live mode, Arrow scope/consent, generated keys, SQL additions and
  independent integration packaging changes. Retain protocol 17, experimental
  Flex/native-read limits, and partial-write/replacement recovery constraints.
- Require complete Test, Release binaries and automatic PyPI publication success.
  Do not set a Go-test skip override. Preserve visible environment/capability skips.
- Verify the expected release assets, SHA-256 digests/sidecars, three wheels,
  stamped versions and native exports/CLI load on the host. Verify PyPI wheel
  hashes against GitHub. Operational logs, endpoints and credentials stay private.
