# Plan and static audit

See [intent](intent.md), [specification](spec.md).

Initial composition focused checks passed1.108s/1.040s before final sparse and
overlay regressions were added. Final results are recorded below.
No library build, Redis/container action, SQL or native timing ran here.

Static broad-source path: scanParallelStarFacts pins eligibility and forces
wp.query="*" plus the full residual. openFlexSnapshotCandidates returnsnil for
"*" before FT.INFO/FT.SEARCH; native workers keep exact-directory scans.
Published segment readiness FT.INFO+ZCARD proofs live in snapshotScope.validated
via validateSnapshotTable, shared across workers/chunks for immutable segments.
Reusing executor caches does not bypass or replace that separate source proof.
All Redis command admission/pin validation and final scope.pin.valid remain.

Per chunk adds one ZLEXCOUNT per source for exact lower-bound seek, a fresh
scanIter/env and directory page head/read setup. The existing wide single-source
boost uses4096 directory IDs; the optional cap clamp avoids discarding roughly
16× directory IDs for a256-row interval. One boundary lookahead page can still
be read, bounded to256 IDs per source. Sparse intervals still cost seeks and
may be empty; the65,536 cap prevents unbounded interval count, not expensive
empty-scan latency. No proof caching/ACL/readiness bypass was added.

CPU-only synthetic scheduling previously measured median53.368ms for256-row
chunks vs66.468ms coarse, but excluded source setup. A2.88m table would require
about11,252 fresh subscans, about21,721 across Q4's three channels. These are
material extra source/control costs. This does not establish a native speedup.

Required final checks: sparse IDs spanning range boundaries/multiple pages,
newer winner/tombstone overlays, missing winner and partial transport errors,
source-error priority after consumer failure, full result cardinality exceeding
the page cap, owned-fact clear/prune and chunk/panic/cancel/progress regressions,
focused race and full unit packages. Root must review and choose any Q4 pilot;
do not publish the native policy before targeted evidence.


Final sparse overlay regression initially exposed a fixture omission: no public
columns were requested, producing empty decoded maps. Explicit id/v demand fixes
the fixture. Sparse/overlay/partial-error plus balanced range checks then passed
0.719s. The original legacy floor-step range accumulated its remainder in the
last chunk; a separate quotient/remainder-spread chunk helper now keeps all
finite chunks and the allocated portion of the tail floor/ceil sized. Existing
coarse helpers remain unchanged; tests cover contiguous bounds beyond2^53 and
near MaxInt64 without intermediate overflow.

Final exact-source all unit-only packages passed with both endpoint variables
unset and GOMAXPROCS=2, `go test -count=1 -p 1 ./...` (root package29.483s).
Focused chunk/parallel/fact race checks passed4.899s. Sparse two-source tests
retain all7 results through cap2 batching, assert the newer winner and tombstone,
and fail closed for a missing winner or partial pipeline error, including later
source error priority after a reducer failure. Directory replies stay≤2 IDs.
The nil-panic subprocess runs with GODEBUG=panicnil=1. These are mocked/local
checks; endpoint integration skips do not prove native compatibility. No native
build, SQL or artifact activation occurred; root review/pilot still required.

Expanded focused race coverage passed5.247s, explicitly including owned native
9,001-row multi-page consumer clear/prune, shared default safety, owned group
representative transfer/cleanup, and capacity-hint regressions. Independent
read-only final review by the owned-fact author found no demonstrated blocker;
that reviewer did not rerun tests.


## Clean publication and validated Q4

The public branch starts at #622 head088324c85f6ca20aeb2a206d0be397d03879a910
and adds ONLY the committed2d2cd45→a494bdc6 chunk delta, excluding private
integration ancestors. The complete staged Go tree is byte-identical to tested
a494bdc6909bdc0935fa0db2fbfb8406a44c929c (zero `git diff` across go/).
Only documentation/provenance differs, so the full unit29.483s/expanded race
5.247s checks above apply without another ancestry-only compile. Redis-dependent
integration skips remain explicit. Both prerequisites are public main-targeted
PRs: [#620](https://github.com/redis/redis-adbc-driver/pull/620) and
[#622](https://github.com/redis/redis-adbc-driver/pull/622). This PR also targets
main; its aggregate diff includes those prerequisites until they land.

Original TPC-DS SF1 Q4 completed on combined source
a494bdc6909bdc0935fa0db2fbfb8406a44c929c, GetInfo v0.0.31-dsfix12,
library SHA256074647dd11c4b5ee2405d54e82ab1cd3e27d1b362dec3f2a7ce305a02694188f.
Warm-up38.604606184s; timed43.049607144/43.873090352/45.976894605s;
median43.873090352s. All three timed outputs were fully fetched and independently
validated against DuckDB:8rows/4columns; DuckDB median0.041623833s.

Environment: Redis Software experimental Flex with verified active4xQPF,
Linux ARM64 client in the server network namespace; client CPU16/GOMAXPROCS16,
query workers32/worker budget32, client memory8GiB/server20GiB. Original retained
SF1 SQL and source data, sequential engine runs, one warm-up/three timed runs,
60-second statement limit. Native11 on the same16CPU profile still timed out
without OOM or timed samples. The passing native12 includes separate ownership
and owned-fact changes; it establishes combined completion/correctness, not
isolated performance attribution to this PR. This is one targeted query, not a
99-query TPC-DS rerun. Full benchmark suites remain stopped; #590 stays open.
