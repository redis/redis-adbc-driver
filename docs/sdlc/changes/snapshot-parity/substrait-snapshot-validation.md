# Substrait snapshot integration

Date: 2026-11-05. Status: targeted and combined snapshot/unit Go checks plus
native Substrait/Arrow/statistics/partition selection pass; broad hosted and
remaining feature/performance parity work stays open. Upstream: main 3aa87e2 (4fd0894, 23860f8 and 3aa87e2), integrated
onto the 8c58522 snapshot checkpoint. See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and [the upstream contract](../213-substrait-runtime/spec.md).

## Required behavior

Keep the upstream bounded Substrait 0.108.0 plan/type/operator/C-admission contract.
Metadata-only SetPlan, Prepare and ExecuteSchema must inspect one committed
catalog and release it. ExecuteQuery/ExecuteUpdate rebind the declared schema
under a fresh generation; a stored plan must not retain a physical layout from
SetPlan. A streamed result retains its execution generation until completion,
release, statement/connection closure, cancellation or deadline. Buffered input
must not bypass an expired reader lease. Materialized results detach after a
complete checked read. SQL and Arrow execution behavior remains unchanged.

Layered tables resolve newest versions and tombstones before Substrait filters,
including rounded Search numerics and exact BIGINT values. Reuse the snapshot
scan paths and residual evaluation, retaining upstream input/output work charges
before filtering. Managed external copies and temporary tables keep their existing
ownership/refresh behavior. No new operators, parameter/partition support, ACL
privileges, storage version, migration or multi-statement transactions are added.

## Implementation and reproduced failures

The PR was CONFLICTING/DIRTY against main. Resolve query iterator fields by
preserving snapshot ordered/candidate/delta readers alongside the upstream
Substrait work budget. ExecuteSchema retains its snapshot pin before either
frontend inspects metadata. The new frontend initially fails the upstream exact
integration test at SetSubstraitPlan with Invalid State / metadata failure:
its binding attempted catalog access outside a statement snapshot (root test
0.18 s). Binding now uses a short-lived core snapshot scope; query execution uses
one fresh scope and transfers its pin to the owned Substrait reader.

The reader validates its lease before returning buffered batches and at EOF,
releases the pin before finishing its execution transport, and registers an
automatic cancellation/deadline cleanup callback after all owners are registered
under its lock. Failures before transfer release the provisional pin. Segmented
sources route through the snapshot-aware scanner even when an ordinary HASH
fallback would scan only one physical prefix.

The first subsequent selection passes the exact/state/lifetime cases and then
fails the upstream budget fixture's unscoped direct metadata read. The budget
fixture now pins its real metadata/source while retaining its complete
20-authoritative-row / 19-remaining-budget rejection. The adopted-schema fixture
also reads stored public columns through a temporary pin; original application
HASH/value assertions remain. No failure is converted to a skip.

## Targeted evidence

All selected upstream and new Substrait cases pass 70 individual race test
events, zero failures/skips; root 10.155 s. After adding an explicit expired-pin
case, the final snapshot-specific selection passes 10 events, zero failures/skips;
root 7.485 s. Production source is the same in both runs; the later addition is
only a test. These counts overlap and must not be summed as unique test coverage.
Both use owned Redis Software 8.8.0, strict TLS, verified active/effective 4x QPF,
noeviction, three primary shards behind its proxy, macOS ARM64 and Go 1.27.3.

New regressions fully fetch/check a layered exact-BIGINT predicate in streamed
and materialized modes, rejecting stale matching versions and tombstones. Four
1201-row fixtures open seven-row batches, then compact/update/drop, ALTER,
TRUNCATE or DROP while GC runs. They check every old visible ID/value/NULL/payload,
uncached source retention, lease removal at EOF and eventual source reclamation.
Repeated metadata operations retain no pins. Cancellation without another
Next/Release releases a live pin; explicitly revoking a pin prevents buffered
rows from escaping while a fresh SQL query still succeeds. Existing upstream
exact decimal/time/binary, temporary/imported tables, schema-change/rebinding,
budget, cancellation/ownership, unsupported-option and wire/semantic checks remain.

Author review covers (1) generation choice, exact visibility and all reader
cleanup paths; (2) reference/namespace safety, unchanged ACLs, preserved import
ownership and error sanitization; (3) supported plan limits and the distinction
between scoped Go results and pending native/hosted/full ordinary evidence.
This is not maintainer approval. Full combined snapshot/unit checks, versioned
native build with the five Substrait C/value/info tests, actual hosted execution,
current Cluster/HA/persistence, isolated reader authorization, public operation
resume and complete sustained performance parity remain open until verified.

## Combined checkpoint

The complete merged-source snapshot race selection passes 382 test events,
24 explicit skips, zero failures; root 279.281 s. Unit-only race verification
passes 1,084 events with 950 expected integration/diagnostic skips, zero failures;
root 53.021 s. The versioned native build passes, SHA-256
`9eabdda2939dfbba9844001dcecaef94e0e0735c79b90d5b90e724b7a94977e6`.
These runs use the final production source; a later connection-close test
addition is being verified separately. The nine Cluster, six reader-model,
six permission/recovery and three diagnostic snapshot skips remain open evidence
gaps. Native execution and hosted current-head results are pending; no new
sustained-performance result is inferred from this frontend integration.

The final snapshot-specific selection, including connection Close, passes 11
race events with no failures/skips; root 7.311 s. Connection Close joins reader
lease cleanup, prevents further reads, preserves a retained caller Arrow batch,
and leaves the committed table readable from a fresh connection. This is a
later test-only addition to the same production source; the combined 382-event
selection above predates it.

The rebuilt versioned native library passes all 118 tests in the five-file
selection, zero skips/failures, 27.29 s. This includes the five upstream Substrait
value/state, feature-info and direct/relocated-library C admission tests alongside
the preceding 113 Arrow/layout/statistics/partition tests. Python 3.14.10,
PyArrow 26.0.1, ADBC manager 1.13.0; the binary hash is the one recorded above.
The Go database process was terminal before fixture reset, strict TLS/policy
reverification and native execution. Current hosted/ordinary/Cluster/HA/persistence
and sustained performance gates remain separate; no complete parity claim.
