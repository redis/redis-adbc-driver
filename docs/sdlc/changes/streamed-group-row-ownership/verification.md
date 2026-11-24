# Verification

Date: 2026-11-24. Status: prototype race, allocation and full unit checks passed; clean publication race and unit checks passed.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

The actual parallel callback panic regression failed before the cleanup fix:
the panic reached the caller and the active grant dropped to zero while a peer
remained gated in canceled native-read cleanup. After adding deferred child
cancel+worker join, expanded ownership/panic/parallel/UNION race tests pass
(5.064s). The recovered panic value is unchanged, the parent context stays live,
worker cleanup precedes token release, and the callback marker is restored.

Other tests cover marker scope on normal/error/cancel/panic callbacks, nested
requests, refused/retained/private/shared-memory fallbacks, source-map isolation,
actual parallel coordinator pruning, shared aggregate inputs and allocation
regression. Synthetic clients use `unused.invalid`; no endpoint is contacted.

Allocation control: both cases produce an identical fresh Q4-width leaf map,
then retain the first group representative with the same eight grouping fields.
The baseline additionally clones that leaf; the owned case prunes/transfers it.
Fresh leaf production is included in both cases so pruning cannot shrink the
next iteration's input. Scratch is reused outside timed iterations.

Environment: Go1.27.4, darwin/arm64, Apple M5 Max, GOMAXPROCS=2,
GOMEMLIMIT=768MiB. Three 300ms samples with `-benchmem`:

| First group representative, including fresh leaf | B/op | allocs/op | Median ns/op |
|---|---:|---:|---:|
| Clone-before-prune control | 7,120 | 44 | 2,447 |
| Proven owned-map transfer | 3,576 | 24 | 1,648 |

These isolate producer/representative work for first group rows; they are not
full SQL timings or a native Q4 speedup claim. No live query, native library
build, Redis/container operation or full benchmark suite ran.

All endpoint-free unit packages passed `go test -count=1 -p 1 ./...` under the
same environment flags (root package 34.110s). After separating the panic guard
and its reusable test fixture, focused ownership/panic race checks passed again
(2.249s). The earlier expanded race pass was 5.064s. The optional ownership delta
contains no source scan/grant lifecycle changes; its prerequisite panic correction
is recorded separately. Formatting, diff and local link checks pass.

Allocation command from `go/`:

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -run '^$' -bench '^BenchmarkGroupRepresentativeOwnership$' \
  -benchmem -benchtime=300ms -count=3 -p 1 .
```

## Clean public extraction

This publication applies only the reviewed ownership delta onto public [PR619](https://github.com/redis/redis-adbc-driver/pull/619) head `16a63469fb9a559208fe559ad02b908d7cbd25b9`. That base contains the public leaf-map production and previously merged main/parallel-panic prerequisites. No private investigation ancestry, new fact-demand/alias/cohort/prebinding production or native benchmark result is copied into this delta.

An independent read-through found no demonstrated blocker: actual star streams have at least one dimension and emit fresh qualified leaf maps; the ownership marker is coordinator/request-local and callback-scoped, restored with defer; worker/nested source requests do not inherit it. All aggregate inputs consume each row before first-group representative pruning. Unsupported/private/retained/shared memory sources retain their clone-before-prune behavior. This is agent review evidence, not human maintainer approval.

The prototype timings and allocation samples above are historical checks of the reviewed delta, not a claim they were rerun on this clean public head. New host-CPU checks were held until the separate root-owned targeted Q4 timing terminated. No successful Q4 result or live performance attribution is claimed.

The subsequent combined native10 artifact (`v0.0.31-dsfix10`, source `99e1aa16ec04d432e4e06ff394df1bd781a381c5`, native SHA-256 `ae254d24b90faa694f0b710f57620b55290def6235e72a3b94bd0e5ff1f44fc0`) still timed out on original SF1 Q4 during warm-up at the configured 60-second deadline, SQLSTATE 57014, with client OOM-kill delta zero. No Redis timed samples or validated Redis output exist for that attempt. Native DuckDB control passed (median 0.048251417 s). This is combined-artifact failure evidence, not a whole-query speedup or isolated attribution to ownership/leaf changes. Full suites remain stopped.

Clean-public focused race checks passed (4.557s), after refreshing onto main with merged PR618/619:

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
 go test -C go -p 1 -race -count=1 \
 -run '^(TestOwned|TestAggregateRepresentative|TestSerialAggregateShared|TestParallelStar|TestStarRowProbe|TestStarProbe|TestUnionStar|TestDeferredUnionStream)' .
```

All clean-public endpoint-free packages passed with the same environment and `go test -C go -p 1 -count=1 ./...` (root package 31.631 s). Endpoint-dependent integration skips are expected and do not establish Software compatibility. Formatting, diff and local SDLC links passed; no SQL/native build/container operation occurred for publication.
