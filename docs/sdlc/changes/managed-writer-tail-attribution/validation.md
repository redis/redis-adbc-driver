# Validation

Date: 2026-11-14. Runtime source b904cf8d, with new test-only instrumentation. PR pending. Runtime optimization deferred because the measured bottleneck is admission waiting, while repeated readiness proofs are a small share of the longest operations.

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -run '^TestManagedTailProfileAttribution$' .`: passed, including eight concurrent operation contexts and production `onceCmd` result handling, maintenance isolation and command-argument omission.
- Exclusive verified Software run of `REDIS_MANAGED_TAIL_PROFILE=true go test -count=1 -p 1 -timeout 20m -run '^TestManagedInterleavedWriterTailProfile$' -v .`: passed in 124.29 s. One warmup and three rotated measured trials for each of 1/4/8 writers, 144 writes/trial. All 12 complete typed row/value oracles passed; 1,728 acknowledged writes; zero failures/conflicts/retries/missing admission observations.
- Verified before live run: active QPF 4, six Search workers, mnp routing, 4 GiB database, certificate-verified TLS, RESP3. All owned clients/workers closed and disposable data/indexes cleared afterward.
- Python summarizer syntax and synthetic validated/unvalidated retention/statistics checks passed. Actual matrix audit: all 12 unique expected trial pairs, 12 final-validation markers, no duplicates or missing admission observations.
- `gofmt` applied. Full Go suite, race build and Python/C-boundary comparison were not rerun for this test-only change. No performance improvement claim.

Cross-review by the unmanaged-fanout subagent found a production-wrapper instrumentation error before accepting evidence; the initial stopped run is invalid attribution and remains private. Corrected full evidence is in [report](../../../../benchmarks/managed-writer-tail/report.md), [sanitized complete samples](../../../../benchmarks/managed-writer-tail/observations.json.gz), and [trial summaries](../../../../benchmarks/managed-writer-tail/summary.json). No credentials, endpoints, SQL, keys or infrastructure identifiers appear in those artifacts.

Post-measurement source clarification: an idle execution pool is pre-created for hooks; cold pool-object creation is excluded. Normal per-statement transport acquisition/registration/finish remains inside ExecuteUpdate timing. Per-call observer allocation/locking can affect scheduling and GC; native/observer-free confirmation is required before adopting a runtime performance claim. Original source labels/raw evidence remain unchanged.

Review: cross-review addressed production-wrapper handling and trial-grid completeness; row mapping, oracle and scope labels inspected. Human review/approval is not inferred. See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
