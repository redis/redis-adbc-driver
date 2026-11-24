# Verification

Date: 2026-11-23. Status: race, allocation and full unit checks passed.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Expanded synthetic probe/parallel/UNION/Flex regression race checks passed in
6.328s, with both Redis endpoint variables unset, GOMAXPROCS=2, Go soft memory
limit 768 MiB, `-race -count=1 -p 1`. No endpoint was contacted.

A frozen copy of the preceding chained-map implementation supplies both the
semantic control and allocation baseline. Benchmark projections contain six fact,
eight customer and two date fields, matching Q4's projected widths. Extra
ownership/provenance test attributes are excluded from the benchmark. The duplicate
case emits 3×4 rows per fact; the late nonmatch emits none. Both implementations
use the same immutable input maps, key normalization, cancellation and escaping
output callback. Worker scratch is constructed once outside timed iterations.

Environment: Go 1.27.4, darwin/arm64, Apple M5 Max, GOMAXPROCS=2,
GOMEMLIMIT=768MiB. Command from `go/`:

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -run '^$' -bench '^BenchmarkStarProbeAllocation$' \
  -benchmem -benchtime=300ms -count=3 -p 1 .
```

| Synthetic probe | Chained B/op | Leaf B/op | Chained allocs/op | Leaf allocs/op | Chained median ns/op | Leaf median ns/op |
|---|---:|---:|---:|---:|---:|---:|
| Q4 projected width, unique match | 6,320 | 3,576 | 42 | 24 | 2,109 | 1,191 |
| 3×4 duplicate Cartesian bag | 50,824 | 42,560 | 302 | 244 | 16,473 | 13,078 |
| Late dimension nonmatch | 2,776 | 32 | 22 | 4 | 1,018 | 85.35 |

The three samples reported identical bytes/allocation counts in each case.
These isolate probe work; they are not native SQL benchmark results, controlled
whole-query memory comparisons or an end-to-end Q4 improvement claim. No native
library build, live SQL, Redis/container action or full suite benchmark ran.

All endpoint-free unit packages passed `go test -count=1 -p 1 ./...` with the
same environment flags (root package 29.751s). Applicable endpoint integration
skips are not live compatibility evidence. Formatting, diff and local SDLC-link
checks passed. The stable earlier parallel/count-proof revisions were unchanged.
