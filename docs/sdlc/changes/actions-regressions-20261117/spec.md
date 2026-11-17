# Specification

2026-11-17. Base: `3671fca7`. Status: implemented and locally validated; replacement CI in progress.
See [intent](intent.md), [plan and evidence](plan.md).

Stale DROP/RENAME and paged ALTER tests must publish a prepared competing
workspace at their existing pause boundaries. Prepare that workspace before the
public statement takes catalog admission, using the existing low-level snapshot
protocol and the originating connection's executor/session/options. This tests
the publication fence independently of foreground admission; public admission
is separately covered by snapshot catalog concurrency tests. Preserve SQLSTATE
40001, immutable-source, temporary-table, rekey, replacement-name, and reclamation
assertions. Never start a synchronously nested admitted write inside a paused
catalog write.

Ray's fixture explicitly expects ARROW:FLIGHT:SQL:TYPE_NAME on every field,
including NUMERIC(12,2) and VARBINARY. Keep check_metadata=True through worker
serialization, retry, snapshot replay, and empty schemas. Do not derive expected
metadata from the returned schema or strip metadata from results.

Acceptance: reproduce the original paged ALTER timeout and Ray mismatch; pass
all changed fixtures and public catalog-admission regressions on Redis Software
with verified 4x QPF, build the native library, and run replacement GitHub CI.
