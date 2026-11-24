# Validation

See [intent](intent.md), [specification](spec.md), [plan](plan.md).

The prototype's parsed-path tests inject metadata through an existing executor metadata map, then run the real parser/binder, cohort registration, CASE/outer row evaluation, subquery invocation/cache, exact directory scanner and native HASH reply consumption. Command hooks never delegate; an unexpected socket dial or command fails. The physical outer-table case also exercises ordinary `streamSelect` and releases its Arrow reader. This explicitly tests optimizer selection rather than accepting correct output from fallback.

Initial focused checks passed, followed by all unit-only Go packages:

```sh
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -run '^TestScalarCohort' -count=1 .
env -u REDIS_URI GOMAXPROCS=2 go test -p 1 -count=1 ./...
```

Full root-package runtime was 28.330 seconds. Applicable Redis-URI integration skips are not compatibility evidence. Coverage includes unused malformed AVG fields (uncached/unfinalized until invocation), rejected-row decode failures, metadata-order/first-page precedence over later source failure, nested CASE and zero outer gates, cancellation, default/explicit NULL fields, integer/decimal exactness and result-type parity, empty/all-NULL AVG, conservative shape/source/cap fallbacks and no cross-snapshot/statement reuse. Source identity and lease failure cases, plus a zero-user-field COUNT scan, are additional focused regressions.

No live SQL/containers/native build were performed. No Q9 success or performance result is claimed. Root independently reviewed the frozen production diff and found no demonstrated blocker in typed-field/default/error order, lazy finalization, source controls, namespace/scope proofs, partition exclusion, cancellation and nonexecuting registration. Immutable artifact/live validation remains separate and pending. Final focused results and the frozen prototype commit are reported in the task handoff.

## Independent multisegment regression

The clean public branch adds a separate reviewer-owned transport fixture for multiple exact source directories. It asserts one cohort scan over an older source plus a newer source, replacement of an older malformed row, suppression of an older row by a newer tombstone (whose malformed metric must remain undecoded), exact visible COUNT/AVG values and no old-version HASH fetches. A corrupt newer row identity must fail closed without caching any successful lane. The regression uses the parsed CASE/outer-table plan and asserts actual activation/physical winner selection; frozen production code is unchanged.

The publication branch is based directly on the public main commit above, with only the reviewed prototype change applied; its private integration ancestors are not included. Standalone public-branch focused cohort tests passed (0.803 seconds), and all unit-only Go packages passed (root package 29.587 seconds). Publication is held until root's targeted Q9 attempt reaches a terminal result.
