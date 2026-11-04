# Specification

2026-11-03; baseline and provenance in [intent](intent.md).
Status: bounded prototype, disabled in normal builds.

## Client stage ownership

An experimental `scanprefetch` build tag enables overlap only for shared streamed
readers with a wide authoritative managed HASH fetch and an active positional
Arrow projection. Generic scans, narrow projections, direct-key reads, tie
sorting and late materialization retain their existing paths. Both ordinary
and public partition readers use `newScanReader`; range transitions preserve
eligibility and start a fresh worker after joining the old one.

The worker owns Search cursor READ, readiness checks and the existing bounded
per-key HMGET pipelines. It does not decode values or access mutable projection
callbacks, eval state or Arrow builders. The consumer owns projection, typed
decoding, defaults, NULL handling, LIMIT/OFFSET and Arrow construction.

An unbuffered handoff allows one consumer-owned reply page and one worker-owned
lookahead page, including blocked delivery. The worker cannot start page three
while page two awaits its consumer. Replies are separately owned Go slices and
strings; no pool reuses them. Existing Arrow builders copy values into buffers
owned by reference-counted records. The bound is by pages, not bytes: arbitrary
large HASH values still increase live memory. No strict byte bound is claimed.

On release, error, LIMIT, range transition or cancellation, cancel the worker,
join it, then close the cursor and release the existing transport/tokens. Cursor
READ/DEL cannot race. Worker-side readiness/READ failures retain existing cursor
cleanup. Context cancellation takes precedence over a concurrently closed queue,
which must not look like successful EOF. Worker panics become stream errors.

Concurrent updates/deletes between discovery and fetch are observed at HASH
fetch time; an existing all-NULL row survives while a vanished row is omitted.
Prefetch changes observation timing and does not imply a snapshot.

## Server recommendation

The [typed export contract](../../../typed-export-design.md) compares four
approaches and specifies v1 schema, values, lifecycle, Cluster and capability
semantics. Recommendation: no-go for production integration until a named Redis
server-owner feasibility decision and matched prototype elapsed gate exist.
No server capability or approval is invented. Keep the stock-server fallback.

## Acceptance

Race tests cover queue bounds, owned replies, cancellation, terminal errors,
mutation after discovery and existing wide/stream/transport lifecycle suites.
A retained-heap diagnostic quantifies the extra page independently of elapsed.
Promotion requires the issue's complete-fetch, independently validated wide and
narrow benchmark matrix on verified 4x QPF Redis Software. Historical timings
do not satisfy this gate. No default enablement before that result.

See [plan](plan.md) and [validation](validation.md) for outstanding evidence.
