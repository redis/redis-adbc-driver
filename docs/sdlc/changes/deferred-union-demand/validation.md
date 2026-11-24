# Validation and review

Date: 2026-11-23. Public parent: `79afa7d47a94460ed37190f7d6c6ee389ab13652` from [PR #603](https://github.com/redis/redis-adbc-driver/pull/603).
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

## Unit verification

```sh
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...
```

All packages passed on the clean stacked branch; root package runtime 28.923 seconds. Tests requiring a Redis URI were not run, and their applicable skips are not Redis compatibility evidence. No live SQL/container or full benchmark run was performed for publication.

The 17 new top-level tests cover structural deferral, restriction before consumer residual evaluation, duplicate and NULL multiplicity, outer join sides, exact integers beyond 2^53, typed decimal-zero constants, pairwise coercion fallback, shared CTE materialization, volatile/error/correlated/lateral fallbacks, ROLLUP, weeks spanning years, alias clones and positional renaming. Dimension tests cover connected-first postponement, saturating/negative/zero/overflow product estimates, conservative source/order fallbacks and independent composite-demand caps/types. Complete original joins and residuals remain exercised.

## Review status and limits

The alias review fixes stale lazy names after explicit derived column renaming, and retains a regression proving a renamed alias does not mutate the original lazy mapping. Additional author passes inspected eligibility, shared CTE boundaries, identity conversions, error-capable expression fallback, source stability, outer/lateral order and composite correlation checks. An independent read-only agent pass reviewed the extracted code and tests and found no demonstrated blocker. It checked alias clones, shared CTE gating, identity conversions, necessary composite sets, saturated dimension products and outer/lateral/opaque-source exclusions. That reviewer did not independently rerun tests or live queries; the checks above were run by the publication agent. It specifically noted that cardinality estimates are not runtime memory guarantees, and selective native reads can change which independent source error is encountered first; observed source/transport errors must still fail closed. Human maintainer approval is pending.

Unit checks do not establish live Q2/Q5 resolution or a memory/latency improvement. The shared weekly Q2 aggregate and unsupported LEFT web-return Q5 branch remain deliberate boundaries. A streaming follow-up will require its own review and live verification. All query issues remain open.
