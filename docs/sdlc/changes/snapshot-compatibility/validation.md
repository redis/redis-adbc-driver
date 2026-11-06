# Validation: snapshot compatibility

Date: 2026-11-02 (America/Los_Angeles). Parent: 7c1a7ca.
PR: https://github.com/redis/redis-adbc-driver/pull/284
Status: locally validated; draft PR, human review pending.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

## Environment and checks

Disposable authenticated TLS Redis Software 8.6.2 / Search 8.6.11, one shard,
noeviction and active 4x QPF verified through the owned Software setup workflow's
management API checks. No other application database was used. Host: Darwin
arm64, Go 1.27.2; module toolchain declaration remains Go 1.26.10.

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -json ./...`:
  all packages passed, 891 passing and 629 expected integration-skip test/subtest
  events. Local loopback access was required by existing TLS/proxy unit tests.
- `REDIS_SNAPSHOT_URI=... go test -race -count=1 -json -run '^TestSnapshot' .`:
  52 passing events, zero failures/skips, 89.564 seconds.
- `make -C go build`: Darwin arm64 C shared library built successfully.
- `REDIS_SNAPSHOT_URI=... python validation/snapshot_validation.py`: all nine
  native C/ADBC tests passed, zero skips, 8.846 seconds. Uses explicit client
  retries for deterministic writes returning SQLSTATE 40001; no driver replay.
- Changed Go files formatted; `git diff --check` and local Markdown links passed.

## Coverage and review

Focused regressions exercise ALTER ADD/default/NOT NULL, type conversion, DROP,
column/table rename, old pinned rows/schema and failed conversion/constraint
rollback. Temporary table/view shadowing and connection isolation survive other
writers; Close removes descriptors while old pins preserve physical rows. A
server-side publication fence rejects cleanup when a temporary owner revives.
Failed temporary Arrow ingestion leaves both the root and name resolution intact.

Adoption/copy/inspection/sweep run beside snapshot catalogs. Imports initialize
beside external data, refresh only matching source origins, never replace an
unrelated table, and preserve application HASHes and indexes. Foreign copies
remain stable until explicit refresh, are read-only, and disappear on refresh
after source-index removal. Prior format 4 is rejected without modification.

Live partitions resolve updated/deleted versions before filtering, survive normal
delta writes, invalidate after TRUNCATE, and retain streamed rows after DROP
without leaking pins. Exact lexicographic directory seeking bounds each live
partition to its row-ID interval. Retained results resolve overlays from one
managed generation and replay after source TRUNCATE/DROP; revocation works.
All previous snapshot/compaction/cancellation/fencing regressions still pass.

Correctness/lifecycle, data ownership, and design/evidence passes followed
REVIEW.md. Review corrected temporary references omitted from internal traversal,
revived-session cleanup races, failed-ingest local name tracking, foreign source
ownership, internal-index discovery, immutable ALTER indexes, and physical-prefix
invalidation of live descriptors. There are no known unresolved findings from
these local checks. This is agent review, not human approval.

## Remaining boundaries

External capture observes a scan and cannot transact with independent application
writers. Pause them during refresh for a point-in-time source image. Atomicity
applies to publishing/reading the managed copy. Foreign refresh is atomic per
index, not across all external indexes. ALTER/imports retain staging/time limits;
retained quotas, existing live query-shape/ID limits and single-endpoint deployment
limits remain. Physical rename/rekey and migration are not added. No benchmark,
full legacy integration-matrix pass, failover certification or green CI is claimed.
