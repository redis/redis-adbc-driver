# Bounded root-map prefetch — native point-only pilot

**NOT SELECTED for root-map prefetch.** These are frozen experimental-source measurements. The broader matrix shows material tail/mixed-workload regressions and drifting controls, so the final selected tree omits prefetch. This does not prove prefetch caused every regression. All measurements remain unchanged; a fresh final comparison is recorded separately.

Date: November 14, 2026. Follow-up to [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

A read-only bounded root-map fetch shares the existing acquisition proof pipeline,
then feeds the existing immutable per-pin resolver state after every original
metadata/lease proof succeeds. This removes one separate root HGETALL exchange;
it does not remove a lease, authorization, descriptor GET or buffered-batch check.
MEMORY USAGE SAMPLES 0 and HLEN guard Lua enumeration. Definite command/ACL/size
declines leave the original native path in place; unknown replies fail closed.

All **four 24-record windows** completed successfully, in baseline/candidate/
candidate/baseline order. Every window retains six cases (managed/unmanaged/
redis-py × one/four clients), one warmup and three measured trials of 32 point
operations. Each case/build has 192 measured observations; the complete pilot
contains 2,304 measured observations plus every warmup. Full typed Arrow/native
results and complete unchanged 1K-row fixture state were independently checked.
Zero failed validations, conflicts, retries or discarded records.

## Observed point latency

Pooled latency milliseconds, execute through complete typed result fetch. Clients
persist within trials; setup, independent validation and teardown are outside
timing. Internal driver work is included. Closed-loop finite workload throughput
is exported in comparison.json; these are not saturated capacity measurements.

| Mode | Clients | Baseline p50 | Candidate p50 | Speedup | Baseline p95 | Candidate p95 |
|---|---:|---:|---:|---:|---:|---:|
| managed | 1 | 3.0393 | 2.7283 | 1.114× | 5.1611 | 4.1442 |
| managed | 4 | 6.0321 | 5.5272 | 1.091× | 15.5897 | 11.7077 |
| unmanaged | 1 | 3.0223 | 2.7483 | 1.100× | 4.7690 | 4.1593 |
| unmanaged | 4 | 5.7206 | 5.4456 | 1.050× | 16.2508 | 16.5482 |
| redis_py | 1 | 0.2433 | 0.2409 | 1.010× | 0.3691 | 0.4250 |
| redis_py | 4 | 0.4937 | 0.4934 | 1.001× | 5.9621 | 6.4856 |

Both single-client driver cases reduce pooled p50 about 9–10%; the four-client
managed/unmanaged reductions are about 8%/5%. All four driver cases improve in
this finite pilot; each individual baseline/candidate window median is retained
in [comparison.json](comparison.json). Redis-py pooled medians are nearly stable.
This does not establish a confidence interval, universal win, or isolation of
server CPU, allocator, cache and scheduling effects. The driver remains about an
order of magnitude slower than redis-py. More work remains beyond this one exchange.

## Exact source and limits

Baseline is clean `7b94a93ccb7bca367c01f0a73204d9cc91a236af`; measured candidate is
clean `762d4ef2` (full commit in provenance). Versioned libraries and CLIs were
built before all timing windows; all pre/post hashes and identical frozen
point-only harness copies are retained. The only harness specialization narrows
POINT to point_read. This is **not** the full 168-record/build matrix and does not
supply ingestion, mutation, aggregate, transfer, ClickBench or product rankings.
The original PR #439 artifacts remain unchanged.

After measurement, separate source `a623a94f` skips prefetch for known restricted
reader pins/stores, avoiding predictable rejected-command/ACL cost. It is not a
capability cache and does not affect eligibility of these ordinary privileged
fixture connections. These samples retain their original candidate source;
they are **not** measurements or full compatibility proof of that later guard.
A later separate failure-path guard `ddb443a0` propagates local pipeline queue
errors and discards alreadyqueued proofs before Exec; the normal successful wire
is unchanged. It also awaits integrated guard checks. The root's final integrated
matrix must record its own frozen source.

Owned Redis Software RAM with active/effective 4× QPF, six Search workers,
mnp/32, one 4-GiB noeviction shard, four-CPU/eight-GiB container and strict
certificate/hostname-verified data TLS/RESP3. Native client CPU/RAM uncapped;
GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset. Actual selected versions/resources are
in [environment](environment.json). Synthetic four-column int64/string fixture
has no NULL/floating-point edges; independent correctness tests cover boundaries.
Managed uses known internal IDs; unmanaged/redis-py uses known application keys,
with address resolution outside timing. Shared/restricted/Cluster API modes were
not timed. No live processes/tests/builds overlapped any native timing window.

## Checks and ownership

[Focused race checks](checks.json) on the measured source: **38 top-level PASS,
zero SKIP, zero FAIL**. They include original acquisition/descriptor/proof/stream/
freshness tests and native bounded-root tests for missing, small, exactly256,
257 fields, large field/value/tail and wrongtype. All unknown/composed failures
are checked before lease digest repair; source cross-review caught and corrected
combined expiry/new-error precedence and unknown-lease diagnostic cases before
these checks. Later restricted-mode eligibility cases require integrated guard
validation and are not relabeled as the earlier 38-pass source.

Each native child process and all its clients exited before exact owner-marker
cleanup of that matrix's keys/indexes. Final fixture data and indexes were empty
when this agent released its slot; the coordinating root retains the shared
owned Software container for other agents and performs final teardown. This
pilot does not claim container/network teardown. Operational identifiers,
credentials/licenses, raw logs and owner markers are private.

Audit offline with `python3 benchmarks/point-root-prefetch/audit.py`.
