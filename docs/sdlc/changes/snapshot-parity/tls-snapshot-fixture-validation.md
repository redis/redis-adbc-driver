# TLS partition fixture and current ordinary suite

Date: 2026-11-05. Parent: 36391aa. Production remains 50d6b84 / protocol 15.
Status: fixture correction and focused checks; restricted-reader parity remains
incomplete. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

The preceding hosted TLS check asks for `idx:<schema>:rows` after ingestion and
deletion. Managed snapshots have immutable physical segments; that predicted
mutable index does not exist. The fixture now produces native partitions with
its declared cap of 137 and reads every partition with the planner principal.
Snapshot acquisition validates retained indexes; the fixture independently
compares all 1,099 logical rows, Arrow schema, decimals, embedded NUL/binary
values and NULLs. Physical segment counts do not prove logical visibility.

Later assertions require original descriptors to retain producer rows through
UPDATE, DELETE, INSERT and TRUNCATE. A fresh producer must observe mutations,
including the later insert. Explicit release must reject new reads. Successful
producers' validated public release IDs are tracked and released before retained
checks or in failure cleanup. Only completed releases leave the list; failures
are not forgotten or automatically replayed within the helper. Explicit-layout
descriptors also join the credential-absence check. Worker ACLs are unchanged.

`python -m unittest discover -s scripts -p 'test_*partition_tls.py'` passes
49 tests, zero skips/failures. Cases include multi-partition merge/schema,
missing/duplicate/deleted/corrupt rows, empty outputs, acquisition/fetch failures,
release ID validation, owned release, idempotence and failed-release tracking.
These isolated tests do not prove server compatibility.

A separate native TLS probe using the final helpers and administrative principal
passes complete 1,099-row/schema/value checks, original-versus-fresh snapshots
across mutations, empty current output after TRUNCATE, retained original rows,
explicit release/rejection and owned schema/group cleanup. This establishes the
revised lifetime oracle against the real library. It uses the ordinary fixture
Search cap and does not prove restricted authorization or cap-137 SELECT support.

The owned Software proxy TLS/RBAC script now passes verified TLS/cap, hostname
rejection, complete planner snapshot values/schema, closed planner and
credential-free descriptors. Its first independent restricted worker then fails
with the fixed diagnostic `permission_denied`. Cleanup removes owned role
mappings and restores the original Search cap; active/effective 4x QPF and
noeviction remain verified. This is progress past the obsolete initial lookup,
**not** a successful restricted-reader run. No EVAL/control-write permissions or
broad row grants were added.

A preliminary ordinary, unbounded `SELECT *` readiness probe at cap 137 instead
failed with `SEARCH_LIMIT_OVER`. Partition readiness now uses the actual bounded
native partition interface. Ordinary SELECT under this Software cap remains a
separate regression to resolve. Cluster distribution/convergence and resharding
helpers still assume legacy physical index/key layouts; their adaptation and
real multi-primary verification remain open. The requirement that fixture rows
span all advertised primaries has not been weakened.

Subsequently, [c06e181](implicit-order-validation.md) reproduces the ordinary
implicit-order cap failure with the preserved preceding native library and
passes the same real-cap, named-principal query with the new library. Explicit
sorting under an undiscoverable cap remains separate work. The original failed
probe and restricted-worker failure above retain their actual scope/source.

The corrected full ordinary race suite at 27e05c2 passes: 2,025 test pass events,
340 explicit skips, zero failures, all six tested packages successful; root
1,158.243 s. Production is unchanged from 50d6b84. The
[failed-refresh record](adoption-compaction-validation.md) retains the earlier
failed attempt and deterministic correction. This ordinary run does not cover
subsequent Python edits or prove skipped capabilities.

Environment: owned Redis Software 8.8.0, three primaries behind its proxy,
strict verified TLS, active/effective 4x QPF, noeviction; macOS ARM64.
Python 3.13.16, PyArrow 26.0.0, ADBC manager 1.13.0. Native library built through
`make -C go build` at 50d6b84, SHA-256
`9471683d6c9b73be51ed5021d801cedec79ff1b49a5479d108809cb8890739a4`.
No concurrent local database workload or benchmark ran during TLS probes.
Operational logs, connection details and role identifiers remain private.

Author review covers exact visibility, capability ownership/cleanup, failure
propagation, unchanged ACL/distribution assertions and the distinction between
planner/admin and restricted-reader evidence. This is not maintainer approval
or completion of remaining parity work.
