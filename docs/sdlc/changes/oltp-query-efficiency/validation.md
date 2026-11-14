# Validation and review

Date: November 13, 2026 (America/Los_Angeles). Requestor: repository user.
Author/reviewer: Codex with statement, managed-publication and aggregate subagents.
Human maintainer review pending. [Intent](intent.md), [spec](spec.md), [plan](plan.md).

[Complete report](../../../../benchmarks/oltp-query-efficiency/2026-11-13/report.md)
and [machine-readable checks](../../../../benchmarks/oltp-query-efficiency/2026-11-13/checks.json).

## Native performance and actual source

Each build completed all 168 standard scenario records, including 42 warmups,
with complete typed Arrow value checks and entire point-mutation post-state
validation. No native failures, conflicts, retries or discarded records. Baseline
ca6858065c4cf02418d55c3aa62eb583590fe252 and candidate
45e0ef69297ecc536b88f690ef5a781218e3cf80 used the same frozen harness and owned
Software fixture. Versioned library/CLI/harness hashes and post-run binary hashes
are retained. The later diagnostic test is test-only; the production diff after
the measured candidate is empty. These are development builds, not a release.

Single-client p50: managed/unmanaged reads 8.655/8.937 to 3.313/3.354 ms;
unmanaged INSERT/UPDATE/DELETE about 4.3–4.5 ms; managed mutations about
41–68 ms and still substantially slower than native commands. Managed filtered
COUNT/SUM improves 11.6x and grouped COUNT/SUM 44.7x against the baseline. All
42 combinations/p95/multiples remain in JSON/CSV, including the observed 10.0%
unmanaged single-client filtered regression and 7.8% managed four-client
projection regression. No universal speedup, causal attribution or confidence
interval is claimed. The broad ordinary tests ran between the two native matrices;
server/client cache, allocator and temperature effects remain possible.

Separate actual 100K-row unmanaged profiling confirms typed inputs are selected,
with authoritative Search/HASH retrieval unchanged. The filtered planned phase
still issues 10,000 HGETALLs; client command wait/decoding dominates. Full grouped
retrieval remains 100K rows. No unmanaged server-reducer permission was added.
Go/race diagnostic times and decode/reduce microbenchmarks are not native timings
or Redis server CPU attribution. No new bulk/transfer, 100M/Flex/Flight or native
Cluster API run was performed.

## Checks

- `make -C go redis-up` configured an isolated Redis Software fixture. Before
  live selections and after all work, independently verified actual Software
  8.2.6-25 / Redis 8.8.0 / Search 80808, active/effective 4x QPF, six workers,
  mnp32, one 4-GiB noeviction shard, strict data TLS/RESP3 and actual four-CPU/
  eight-GiB container limits. Native client Apple M5 Max/macOS arm64, uncapped,
  GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG unset; Go tests used GOMAXPROCS=4.
- Broad ordinary `go test -v -count=1 -p 1 -timeout=30m ./...` on checkpoint
  56890a35: 1,145 top-level passes, 367 skips, zero failures. Snapshot URI was
  deliberately unset; these were run separately. Later cleanup guards were
  verified in the final focused/snapshot selections rather than relabeling this
  checkpoint as the final build.
- Final combined `go test -v -race -count=1 -p 1 -timeout=10m -run <focused> .`
  on measured 45e0ef69: 33 passes, zero skips/failures. Includes pool ownership,
  cancellation/Close, stale contexts/in-flight commands, pipeline unknown outcomes,
  WATCH/UNWATCH and MULTI/EXEC cleanup, prepared schema/data freshness, unmanaged
  live definition checks, runtime exact aggregates and publication/clone bounds.
- Separate complete `go test -v -count=1 -p 1 -timeout=30m -run '^TestSnapshot' .`
  on 45e0ef69: 295 passes, 62 skips, zero failures. The full skipped-name lists and
  exact commands are retained; opt-in profiles, missing topology/backup environments
  and Software capability limits are evidence gaps, not hidden compatibility proof.
- Opt-in 100K unmanaged aggregate profile on test-only 44e9c1e2: PASS, full
  Arrow INT64/value/count validation and actual typed-input plan assertions.
- Five scenario and three comparison Python tests PASS. Syntax-only repeated SQL
  benchmark removes parse allocations; execution still rebinds schema/types.
- Offline evidence audit and `git diff --check` pass. No production behavior was
  changed after native timing; only the attribution test and documentation/evidence
  were added. The comparator regenerates all 42 chart rows exactly.

## Review and cleanup

Cross-review covered correctness/lifecycle, security/data ownership and actual
claims versus evidence. Findings fixed before final native timing: pre-enumeration
metadata-map bounds, stale execution contexts, in-flight pool admission, native
first pipeline errors hiding later lost replies, and refused transaction cleanup
leaving WATCH/MULTI state. The latter two protocol cases fail against the preceding
implementation and pass after correction. Original cleanup argument capture was
preserved; optional diagnostic maps are not constructed when disabled.

Retain immutable snapshot visibility, native exactness/fallback, complete cursor
and Arrow ownership, source ownership, permissions, explicit sweep/capacity
progress, canceled/unknown outcomes and no replay. No storage-format change or
new public option. Review does not authorize merge; human review remains pending.

All clients/test processes ended, data/indexes were empty and QPF/TLS/RESP3 were
verified before removing owned fixture/network and generated credentials/environment.
Preexisting container/networks and TLS material were preserved. Public evidence
contains no endpoints, operational identities, credentials/licenses or private
logs/CPU profiles. The cleanup proof and selected provenance are retained.

Review: [PR #437](https://github.com/redis/redis-adbc-driver/pull/437). Human maintainer approval remains pending.
