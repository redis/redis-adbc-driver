# Evidence and review

2026-10-21 (America/Los_Angeles). Started from `1282e4a`; integrated merged
main `a90c6b4` before final validation. Author: Codex; requestor: Albert Wong.
This verifies private storage only. All four runtime issues remain open.

## Checks

- Standalone: `GOTOOLCHAIN=go1.26.6 REDIS_URI=redis://127.0.0.1:16482/0 REDIS_PARTITION_CAP_TEST=1 go test -race -count=1 -p 1 -run '^Test(RetainedPartition|Partition)' -v .` from `go/`: **33 passed, zero skipped**.
- Three-primary OSS Cluster: same flags, URI port 16601, `-run '^TestRetainedPartition'`: **12 passed, zero skipped**.
- Unit-only `env -u REDIS_URI -u REDIS_ADMIN_URI GOTOOLCHAIN=go1.26.6 go test -count=1 -p 1 ./...`: all three packages passed. Integration skips here are expected; separate configured runs above provide Redis coverage.
- Shared library rebuilt with `make -B -C go build`, Go 1.26.6, macOS deployment target 12.0, Makefile version linker flag. Full configured native Python suite: **372 passed, 8 skipped, 3 subtests**. `scripts/check_partition_junit.py` confirmed all **10 partition cases**. The new private store is not yet reachable through ADBC.
- `gofmt` and `git diff --check` passed. Logs, JUnit, exact source/library hashes and fixture versions are in [evidence/](evidence/).

Storage regressions exercise publication and fresh-reader exact Arrow IPC replay
after the publishing client closes (int64 above 2^53, MaxInt64, binary/NUL,
NULL), concurrent quotas, unused reservation reuse, orphan/committed expiry,
revocation races, incomplete publication, checksum corruption, mid-read manifest
eviction, namespace policy conflicts, read-only ACLs and cross-tenant denial.
Retained control state stays out of all-prefix HASH application indexes.
Four focused before-fix failures are preserved alongside the final passing runs.
The first disposable Cluster launch lacked per-node startup readiness and failed
before test setup; it was removed, recreated with readiness checks and all three
primaries verified healthy before the recorded final run.

## Cleanup

All owned namespace/application keys, indexes and ACL users were verified absent
on standalone and each Cluster primary. The native helper verified its owned PID
and stopped it; the exact label-verified disposable Cluster container was removed.
No paid or Redis Cloud resources were created. The Redis endpoints were local,
plain TCP fixtures; these runs make no new TLS or Cloud compatibility claim.

## Review

Self-review followed the three passes in [REVIEW.md](../../../../REVIEW.md):
state/TTL/quota races and partial publication; key/ACL/application ownership;
then scope and evidence. Findings corrected before final validation: manifest
revalidation between chunks, full hard-limit revocation, String-only control
metadata to avoid application Search pollution, and immutable provisional lease
checks. No unresolved reproduced foundation defect remains. This is an agent
review, not maintainer approval.

Remaining work includes the bounded global SQL coordinator, public options and
portable descriptors, validated bounded Arrow decoding, reader-only bootstrap,
explicit revocation, independent-process replay, additional layouts/large IDs
and performance evidence. This PR must not close #359, #360, #362 or #363.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
