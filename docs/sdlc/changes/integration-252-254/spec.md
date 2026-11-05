# Specification: candidate integration follow-ups

Date: 2026-10-30. Source: `91064e6`. Status: implemented locally.
[Intent](intent.md) · [Plan](plan.md) · [Validation](validation.md)

## Contracts

- R tests assert empty parameter field names and count, then bind/fetch twice.
  Cover one integer, multiple integers, NULL text and mixed NULL/integer inputs.
  Retain the strict adbi 0.1.3 exact-message expected failure until a supported
  upstream package passes. The proposed adbi patch recognizes empty schema field
  names as positional; its tests retain named-input/count rejection.
- Datasets tests independently exercise default persistence using a local typed
  Arrow table and fetched ADBC results. Only Datasets 5.1.0's exact IndexError
  with an empty `_data_files` list is expected. A fix produces a strict XPASS,
  requiring review. Retain an explicit-shard empty-result test; populated data
  uses default sharding. The upstream patch ensures at least one shard.
- `ray-tls` is an exclusive selector. It provisions a fresh Compose project,
  verifies 4x QPF before/after TLS conversion using the existing Software fixture,
  and runs the datasource suite with its authenticated `rediss://` endpoint and
  per-connection private CA. A remote task must have a different PID and return
  every row with exact Arrow metadata. Removing CA trust must produce certificate
  rejection. Missing TLS configuration fails this profile instead of skipping.
- Pre-existing projects are rejected before mutation. The shell waits for pytest,
  removes its own container/network and private TLS directory on success/failure,
  and preserves a failing exit status. Short tracebacks omit fixture argument
  representations, including the authenticated URI. No global trust is changed.
- The all-native launcher includes a separate `ray-tls` environment/report. JUnit
  summaries retain pass, failure, error, skip and xfail counts independently.

## Boundaries and risks

Two fixes belong to external packages. Bundled patches and local verification
are not upstream acceptance or releases. The default test environments remain
unpatched; R/DBI binding remains unsupported there. TLS is local single-host
Software coverage, not multi-node Ray deployment validation.
