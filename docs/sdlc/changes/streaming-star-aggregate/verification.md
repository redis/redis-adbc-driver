# Verification

Draft implementation: [PR607](https://github.com/redis/redis-adbc-driver/pull/607).

Date: 2026-11-23. Status: standalone checks passed; live evidence pending.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Origin: streaming follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606).
This branch contains only streaming code/tests and these change records; no
private investigation ancestry, Flex implementation, typed scalar residual
implementation, infrastructure artifacts or benchmark data are included.

Tests use in-memory rows and self-contained ordinary managed-directory mocked
transport. `unused.invalid` is never contacted. Parsed regressions assert actual
stream selection for ordinary tagged join plans and projected UNION weekday
CASE aggregates; functional answers alone are not proof of optimizer selection.
Tests retain duplicates, exact decimal sums, NULL cases, hidden provenance,
positionally renamed columns, complete boundary weeks, shared CTE materialization,
page bounds, cancellation and later source failure priority. Unsupported presence
and retained-budget dependencies keep their existing path before consuming input.

No live Redis, SQL endpoint, container operation or full benchmark suite is part
of these checks. Integration skips are not current Redis compatibility evidence.

Standalone focused parsed/UNION/star checks passed (1.039s). All packages passed:

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -count=1 -p 1 ./...
```

The root package completed in 29.365s. Applicable integration tests skip without
Redis endpoints; no live compatibility conclusion follows. `gofmt`,
`git diff --check` and SDLC local-link checks passed. Independent review prompted
explicit no-consumption fences for retained-allocation contexts and private owned
native sources; focused regressions cover both.

Focused native status supplied by the benchmark investigation: the Redis
Software Flex SF1 streaming pilot still timed out during Q4 warm-up at 60 seconds,
with OOM delta 0 and no successful Redis latency sample. Q8 also timed out at
60 seconds with OOM delta 0 in that combined pilot. A separate short diagnostic
confirmed Q4 stream selection; it is not a benchmark sample or a controlled
memory/speed comparison. Q2, Q4 and Q7 are not claimed resolved. This public
branch additionally fences private owned native sources, absent from the pilot,
so the exact public head has not been natively validated. Full suites stay stopped.

After rebasing onto merged PR606 on `main`, all standalone packages passed again
(root package 28.823s). The same endpoint-free command above was used.
