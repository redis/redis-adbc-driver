# Validation: build each private output index once

Date: 2026-11-02. Parent checkpoint `9bd2bc9`. Status: implemented and tested;
remaining [parity work](plan.md) is still open.

## Change and correctness boundary

CREATE/CTAS, SQL/Arrow insertion, UPDATE/DELETE, ALTER rewrites and TRUNCATE defer
private Search indexes until their rows are sealed. Private planning uses
visibility-aware directory reads; immutable input indexes can still supply
selective candidates for empty private tails. Capability discovery routes through
an existing input index and prefix proofs skip empty owned tails. Final indexes
still use distributed top-level CREATE with server-side admission, full initial
scan and readiness/error/count checks before atomic publication.

A statement-local record preserves each output's eventual baseline/overlay flag,
including any tombstones written while staging. It is removed after final index
installation; no on-disk format change is introduced. Unchanged shared indexes
and the separate durable compaction copy/index/install phases are preserved.

No-op UPDATE/DELETE with zero row-image/tombstone writes returns without a new
publication or index build. This still copies private metadata before discovering
that no rows match; incremental metadata work remains necessary.

## Evidence

The pre-change regression measured two FT.CREATE commands for a one-row UPDATE.
The fixed case measures one, validates the changed row, exact count and sum.
Additional cases measure one final index for INSERT, DELETE, ALTER, TRUNCATE,
CTAS and an empty CREATE. The no-op regression initially measured one index;
the fix measures zero, returns no changes and preserves the root pointer.

The private-read/readiness fault test proves the private output index is absent,
reads the shared input and then a newly inserted unindexed row, and confirms that
publication waits through an injected rebuilding response. A private baseline
insert followed by deletion remains empty after publication, protecting
COUNT(*) against exposing a tombstone as a baseline row.

Owned Software fixture: same local three-primary-shard proxy topology described
in [the read record](validation.md), verified TLS, active verified 4x QPF and
noeviction before these runs. Go 1.27.2; `make build` native library. Suites ran
sequentially against the disposable database.

- Complete `go test -race -count=1 -v -run '^TestSnapshot' .`: 76 pass events,
  seven expected Cluster API skips, no failures; 102.561 seconds.
- All unit packages, `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -json ./...`:
  892 pass events, 659 expected integration skips, no failures.
- Native `validation/snapshot_validation.py`: all 10 tests passed, 8.376 seconds;
  includes late-input atomicity, ingestion, ALTER/temp objects, external refresh,
  stream/compaction lifetime and live/retained partitions.
- `gofmt` and `git diff --check` passed.

The command-count reduction is measured. No write-latency/throughput percentage
is claimed before comparative write benchmarks. Earlier read samples belong to
`9bc80c0`, not this later implementation.

## Review and remaining work

Author self-review checked private read routing, shared-input immutability,
tombstone tracking, restoration of baseline metadata, final readiness checks,
no-op result/cancellation paths and ownership/reference cleanup. This is not
maintainer approval. Existing protocol fences remain in place.

Inspection of PR CI for the preceding checkpoint found ordinary-client
compaction-admission retry errors, legacy raw-metadata/ownership test
assumptions, and an IPC test that expects partial late-error ingestion. They
remain explicit full-feature work; this milestone does not claim green platform
CI. Incremental metadata/publication, large atomic operations, sustained
compaction, updated full feature/platform/Cluster recovery validation, broader
benchmarks and owned cleanup remain required by the active goal.
