# Validation

See [intent](intent.md), [specification](spec.md), [plan](plan.md).

The prototype's parsed-path tests inject metadata through an existing executor metadata map, then run the real parser/binder, cohort registration, CASE/outer row evaluation, subquery invocation/cache, exact directory scanner and native HASH reply consumption. Command hooks never delegate; an unexpected socket dial or command fails. The physical outer-table case also exercises ordinary `streamSelect` and releases its Arrow reader. This explicitly tests optimizer selection rather than accepting correct output from fallback.

Initial focused checks passed, followed by all unit-only Go packages:

```sh
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -run '^TestScalarCohort' -count=1 .
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...
```

Full root-package runtime was 28.330 seconds. Applicable Redis-URI integration skips are not compatibility evidence. Coverage includes unused malformed AVG fields (uncached/unfinalized until invocation), rejected-row decode failures, metadata-order/first-page precedence over later source failure, nested CASE and zero outer gates, cancellation, default/explicit NULL fields, integer/decimal exactness and result-type parity, empty/all-NULL AVG, conservative shape/source/cap fallbacks and no cross-snapshot/statement reuse. Source identity and lease failure cases, plus a zero-user-field COUNT scan, are additional focused regressions.

No live SQL/containers/native build were performed. No Q9 success or performance result is claimed. Root independently reviewed the frozen production diff and found no demonstrated blocker in typed-field/default/error order, lazy finalization, source controls, namespace/scope proofs, partition exclusion, cancellation and nonexecuting registration. A separate combined native artifact has now passed targeted Q9 validation; attribution boundaries and samples are recorded below. Final focused results and the frozen prototype commit are reported in the task handoff.

## Independent multisegment regression

The clean public branch adds a separate reviewer-owned transport fixture for multiple exact source directories. It asserts one cohort scan over an older source plus a newer source, replacement of an older malformed row, suppression of an older row by a newer tombstone (whose malformed metric must remain undecoded), exact visible COUNT/AVG values and no old-version HASH fetches. A corrupt newer row identity must fail closed without caching any successful lane. The regression uses the parsed CASE/outer-table plan and asserts actual activation/physical winner selection; frozen production code is unchanged.

The publication branch is based directly on the public main commit above, with only the reviewed prototype change applied; its private integration ancestors are not included. Standalone public-branch focused cohort tests passed (0.803 seconds), and all unit-only Go packages passed (root package 29.587 seconds). Root authorized publication after the targeted Q9 attempt reached a fully validated terminal result.

## Targeted Q9: combined-artifact observation

The original SF1 Q9 returned **one row and five columns**, fully fetched and independently validated for all three timed outputs against the native DuckDB warm-up. This uses compiled combined source `119633325dc33d8fd633c3bfea71375ba43b245b`, native GetInfo `v0.0.31-dsfix6`, library SHA-256 `200168f2dd888982b33353f23ae7faf46cff5bf4c5f784f84a0db13f15fd6249`.

| Measurement | Seconds |
|---|---:|
| Redis warm-up | 57.795611276 |
| Redis timed run 1 | 30.745336598 |
| Redis timed run 2 | 43.636604686 |
| Redis timed run 3 | 29.511384347 |
| Redis median | 30.745336598 |
| Native DuckDB median | 0.038002291 |

Profile: Redis Software 8.8.0 Flex experimental stopgap, verified active 4× QPF, six Search workers, mnp routing and 32 connections; logical capacity 64 GiB and Flex RAM cache 8 GiB. Linux ARM64 client in the server network namespace, four client CPUs with an 8 GiB client ceiling; Software container ceiling 20 GiB. Original SF1 fixture/SQL, one warm-up and three timed runs, configured 60-second statement deadlines, fresh sequential engine processes and separate disk-backed validation. The 57.796-second warm-up is close to the configured limit; no timing-margin guarantee follows. Full suites remain stopped.

The live combined source also contained separate LEFT-join, fact-demand, Flex-cap and streaming work. Those changes and their private integration ancestry are excluded from this clean cohort PR. This is a validated **combined-artifact** Q9 result, not a standalone pure-cohort SF1 measurement. Unit tests prove cohort registration/activation and scan sharing independently, but a separately built clean-PR live test would be needed for isolated performance attribution. [Q9 #595](https://github.com/redis/redis-adbc-driver/issues/595) remains open for maintainer review.

## Independent source-proof finding and regression

Independent review identified [#609](https://github.com/redis/redis-adbc-driver/issues/609): `fetchHashCommands` previously accepted any pipeline error matching `errors.Is(err, redis.Nil)`. A synthetic, nondelegating transport test on the actual parsed cohort path first supplies valid, error-free HMGET values, then returns a composed Nil plus reply-loss error (or wrapped Nil). Before the correction, both cases incorrectly returned success and cached two member results. The direct-Nil control still succeeds. After the one-line direct-equality correction, all focused cohort tests pass (0.709 seconds), including uncertain-error/no-cache assertions. The reviewer checked the specific guard and regression and found no remaining issue there; this is not a claim that other helpers were audited.

The observed native6 Q9 run had no reported ambiguous transport failure and retains its actual validated output/source provenance above. Its binary predates this stricter error guard; a separately rebuilt publication branch is required for exact published-source performance attribution. The synthetic finding is not presented as a live Q9 failure.

After HASH-read hardening, the final standalone `./...` unit-only pass succeeded for every package (root package 28.691 seconds).

Reviewable implementation: [PR #610](https://github.com/redis/redis-adbc-driver/pull/610).
