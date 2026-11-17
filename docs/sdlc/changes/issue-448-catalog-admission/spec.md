# Independent catalog writes: specification

Date: 2026-11-16. Baseline: `225e0056`. Status: implemented and validated locally; pending maintainer review.
See [intent](intent.md) and [plan](plan.md).

SQL writes without a single DML target and Arrow create/replace/create-append
with an absent target acquire database-wide catalog admission on their existing
renewable writer pin. This deliberately serializes catalog execution, including
CTAS source work and final indexing. Existing-table DML/Arrow append retain their
per-table admission. Catalog admission is exclusive with admitted table writers,
including those on other tables, because their planning may read membership
metadata. Disjoint table writers still run concurrently; reads remain independent.
There is no fairness guarantee.

Admission atomically requires a live writer pin at the current root and no other
live catalog or admitted table writer. Table admission also rejects a live
catalog owner. Unadmitted pins never block one another. A definite refusal
releases the pin, waits with bounded
cancellable backoff and resolves a fresh snapshot/target before another attempt.
No workspace, SQL/default evaluation or Arrow consumption precedes admission.
Unknown admission replies fail without replay. Release, successful publication
and expiry relinquish admission through the existing writer-pin lifecycle;
renewal preserves it. No new Redis key or storage version is required.

Catalog admission does not weaken publication dependency validation, lease
fences or durable outcome resolution. Prepared retained operations release their
pin and remain subject to normal dependency conflicts when later resumed.
Older clients do not participate in this new admission: mixed-version concurrent
DDL is not covered by the guarantee. Direct import/registration APIs retain their
existing publication conflict handling; this change coordinates SQL, managed Arrow
and temporary-object cleanup. SQL compatibility and data ownership stay unchanged.

Checks: four native concurrent connections create schemas, same-schema CTAS and
views, rename and drop distinct objects, and verify exact results. Arrow checks
cover waiting, cancellation, single consumption, fresh OID allocation and lost
admission replies. Existing disjoint publication, dependency conflict and lost
publication reply tests must continue to pass. Validate the native dbt adapter
with four model threads and parallel pytest with caller SQL retries disabled.
