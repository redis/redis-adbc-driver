# Issue #365 acceptance evidence

Date: 2026-11-10. Source baseline: `018e0368` (merged #369 and #373).
Status: native driver acceptance complete in released v0.0.24.
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md),
[verification](verification.md), [proposed release notes](release-notes.md).

| Criterion | Evidence and state |
|---|---|
| Exact API, ownership, authorization, bounds, failure contract and Python two-table example | Implemented/documented in [shared read snapshots](../../../shared-read-snapshots.md) and [driver options](../../../driver-reference.md). |
| Metadata and sequential/streamed reads during DML, schema/view changes, TRUNCATE/DROP | Merged Go shared-view tests and #373 metadata-depth regression; [feature evidence](../shared-read-snapshot/verification.md) and [metadata evidence](../shared-read-snapshots/validation.md). |
| Complete schemas, exact values/multiplicities and fresh ordinary reads | Same Go/native regressions compare the original generation with complete fetched results; ordinary writer reads observe new publications. |
| Expiry, lease loss, quotas, cancellation, early close, parent release, process death and collection | Original feature evidence includes natural two-minute process-death/lease collection and race tests; metadata regression forbids success at every tested depth/filter after loss. |
| Authorized sharing/partition workers, or explicit rejection | Connection-scoped only; attachment, ExecutePartitions and ReadPartition reject unsupported use in merged tests. |
| Authenticated TLS Software/Search/noeviction/4x-QPF evidence and exact source | Source evidence above plus [published verification](verification.md#published-v0024-verification). Tagged Software/Test, binary and PyPI workflows passed; published checksums and clean installations were verified. Flight SQL is explicitly unsupported; no gateway evidence is inferred. |
| Persisted compatibility | Uses existing protocol-17 pins; no new format or legacy upgrade path. |
| Released version and safe connector integration steps | [v0.0.24](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.24) and [PyPI 0.0.24](https://pypi.org/project/adbc-driver-redis/0.0.24/) are published. [Connector steps](../../../shared-read-snapshots.md#airbyte-and-flight-sql), binary/PyPI completion, checksums and clean native/PyPI installation evidence are documented. |

The current external connector still rejects shared_snapshot. Driver release does
not by itself enable that connector setting; deployment must implement/test the
native lifecycle and pin the matching artifact. An earlier Airbyte discover call
is not the read attempt's pinned catalog: read must rediscover and validate all
selected schemas after acquire and before its first record.
