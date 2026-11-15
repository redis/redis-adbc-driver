# Validation and selection

[Intent](intent.md), [specification](spec.md), [plan](plan.md),
[complete report and evidence](../../../../benchmarks/unmanaged-projection/2026-11-14-field-args/report.md).

The runtime experiment is not selected. Final experiment source `6b1806b2` reduced
batch CPU/allocation, but four-client Go SQL p50 increased in both separately
executed pairs. Production reader and existing unit file are restored exactly to
`b904cf8d`; only opt-in harnesses, audits, docs and archived evidence remain.

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4 go test -count=1 -p 1 -run '^TestUnmanagedProjectionRead' -v .`: earlier `7831d016` 17 pass/3 Software skips; matching race run same. These are not live compatibility proof.
- Final `6b1806b2` matching `-race` check: 18 pass/3 Software skips, including independent mutable command-argument ownership; its unit source is archived rather than added for an omitted optimization.
- Verified Software final focused `go test -count=1 -p 1 -run '^TestUnmanagedProjectionRead' -v -timeout=15m .`: 21 pass/0 skip/0 fail, including exact values, NUL, NULL fallback, live replacement, large native values, permission/unknown-error guards and ownership regression.
- Final batch benchmark: baseline/candidate/candidate/baseline, same harness, six observations/source/case, all 72 observations retained. 1000-key CPU medians −8.2/−21.0/−36.2% for 2/16/128 fields. Single-key allocations/bytes unchanged; no end-to-end claim.
- Both baseline/final sparse matrices: 64 records/512 independently validated reads each, 384 measured each. All native source-command counts match. Earlier separate-template candidate also retained.
- Full Go SQL: 32 records/1024 completely fetched/typed/oracle-checked queries including warmups; 768 measured. Forward B→C used `go test`; reverse C→B rebuilt both executables once before windows and then directly invoked their frozen hashes. No rebuild within reverse windows. These are separate command-form pairs, not one frozen-binary ABBA. Raw arrays/pairs retained independently.
- Four-client p50: forward 84.883→97.201 ms, reverse 90.902→94.999 ms. COUNT/SUM source fanout unchanged: 320,000 source+64 metadata HMGET/trial. Endpoint CPU attribution and native C-boundary benchmark were not performed.
- `python3 -m unittest discover -s benchmarks/unmanaged-projection -p test_sparse_audit.py -v`: 2 tests pass, including six corrupt matrix cases. Auditor accepts both complete baseline/final matrices. Independent SQL grid/count check accepts all four 8-record invocations.
- `git diff --check`: pass before delivery. No full package suite or performance claim for unselected production code. No further Go builds or Redis tests run after releasing the shared slot; final integration checks remain the parent's responsibility.

The Software helper verified active/effective 4× QPF, 6 workers, mnp/32, one 4 GiB shard,
noeviction, TLS/RESP3 before every live command. All clients/processes ended and the
snapshot harness attempted disposable index/data cleanup before slot release;
its individual cleanup errors are ignored, so this is not an independently
verified post-run emptiness claim.
Private operational details stay outside git. Agent selection is not human review.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
