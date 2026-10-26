# Specification

2026-10-21; #390; see [intent](intent.md), [plan](plan.md).

- After successful FT.ALTER, await the existing strict readiness check on every
  OSS Cluster primary or the Software/proxy endpoint before publishing ALTER
  metadata. Only incomplete/building status retries, at 10ms poll intervals.
  Cancellation/deadline, permission, transport and unsupported readiness errors
  propagate. There is no arbitrary completion sleep or new timeout option.
- Failed/cancelled pre-commit waits leave metadata unchanged; unused physical
  index attributes can remain under the existing orphan-attribute rules. Retry
  chooses fresh fields. TRUNCATE's concurrent-column catch-up retains pending
  readiness across retries without probing unchanged empty indexes.
- While metadata records dropped-column cleanup, ordinary scans and aggregate
  inputs use complete HASH reads and full residual/global SQL evaluation.
  Disable index/parallel aggregate shortcuts during that interval. Preserve
  direct-key reads. Keep background cleanup asynchronous.
- Permanent managed cleanup reads enumerate the captured allocated-ID watermark
  in <=1000-key HASH pages, with cancellation and prefix checks before/after
  fetches. No SCAN grant, Search double boundaries or global dedup set is needed.
  Sparse allocated domains can require more requests. Normal blocking SQL can
  still materialize results; page bounds are not a whole-query memory limit.
- Search-backed ReadPartition fails INVALID_STATE while cleanup is pending;
  descriptors remain retryable after cleanup. Proven direct HASH reads stay
  available. Balanced planning retains cheap equal cuts during cleanup.
- No new SQL/layout, credentials, snapshot, or general concurrent/external DDL
  guarantee. A readiness barrier mitigates the reproduced backfill interval;
  it is not a universal guarantee against every Search/backend defect.
