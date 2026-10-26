# Evidence and review

2026-10-21 (America/Los_Angeles). Author: Codex; requestor: Albert Wong.
Integrated main `0a84794` (merged #423 foundation and #422 wide-HASH changes).
This verifies the private retained reader; public retained execution stays gated.

## Validation

- Standalone: from `go/`, `GOTOOLCHAIN=go1.26.6 REDIS_URI=redis://127.0.0.1:16483/0 REDIS_PARTITION_CAP_TEST=1 go test -race -count=1 -p 1 -run '^Test(RetainedPartition|Partition)' -v .`: **44 passed, one helper skipped** in the parent. The helper actually executes in two subprocesses, each validating exact replay after the publishing client closes.
- Three-primary Cluster: same flags, URI port 16701, `-run '^TestRetainedPartition'`: **23 passed, one helper skipped** in the parent; two subprocess replay checks also passed.
- Unit-only `env -u REDIS_URI -u REDIS_ADMIN_URI GOTOOLCHAIN=go1.26.6 go test -count=1 -p 1 ./...`: all three packages passed. Integration skips here do not prove Redis coverage; the configured race runs do.
- Final `go test -count=1 -p 1 -run '^$' -fuzz '^FuzzRetainedPartitionIPC$' -fuzztime=20s -parallel=2 .`: **284,474 cases, zero failures**. Earlier development run: 283,613 cases. Both logs retained; fuzzing is bounded sampled evidence, not an exhaustive safety proof.
- Rebuilt shared library via `make -B -C go build` with Go 1.26.6, macOS deployment target 12.0 and the Makefile version flag. Full configured native Python suite: **372 passed, 8 skipped, 3 subtests**; `scripts/check_partition_junit.py` confirmed all **10 required partition cases**. Public behavior remains live-only, so this is regression evidence for the shared stream-owner change.
- `gofmt` and `git diff --check` passed. [Metadata](evidence/metadata.json) records exact source/library hashes and fixture versions. [Logs/JUnit](evidence/) retain outcomes/skips.

## Coverage and findings

Exact int64 values above 2^53 and MaxInt64, binary/NUL/NULL, empty results, SQL
output types and nested list/record output are checked. Readers split chunks,
allow retained batches after Release, and release local state on cancellation,
owner close, normal completion and concurrent close/read races. Shared jobs stay
replayable after local cancellation/release. Checks stop at chunk boundaries for
revocation, eviction, integrity-valid wrong schema/rows, corruption and expiry.
A separately authenticated reader uses namespace-only GETRANGE/TIME permissions;
write/revoke and cross-tenant reads remain denied. It loads the persisted policy
without write/bootstrap privileges. Descriptor tests reject unknown fields,
trailing JSON, arbitrary keys, wrong versions/namespaces/IDs and invalid ranges.

Framing/preflight tests reject every truncation of a valid stream, trailing
bytes, invalid root/metadata lengths, huge vectors/row counts/body declarations,
invalid buffer spans and LZ4/ZSTD compression before Arrow reconstruction. IPC
schema strings/vectors/field recursion are bounded, as are nodes/elements and
body bytes. Message buffers alias verified chunk bytes; decoding does not allocate
an encoded body or decompress buffers.

A focused test found Arrow Go's IPC NULL decoder supplies zero buffers while
its full validator expects a nil placeholder (`expected at least 1 buffers for
null, got 0`). The reader normalizes only that private decoded NULL layout before
full validation, including nested NULLs; preflight independently checks original
NULL node counts. No column validation is skipped. The case passes after the fix.

## Review, cleanup and limits

Self-review followed [REVIEW.md](../../../../REVIEW.md): correctness/lifecycle,
then key/ACL/application ownership, then intent/evidence. No unresolved reproduced
component defect remains; this is not human maintainer approval.
All owned retained/application keys, users and indexes were absent on standalone
and each Cluster primary. The PID-verified native fixture and exact label-verified
Cluster were removed. No paid/Cloud resources were created. Plain local endpoints
do not establish new TLS/Cloud compatibility or performance claims.

The public ADBC API is unchanged: no reader-only connection option, public retained
ExecutePartitions mode, global SQL coordinator, broader layout support or public
revocation is enabled here. Those and their API/failure/performance checks remain
required for #359/#360/#362/#363. Logical IPC bounds are not a process RSS limit.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
