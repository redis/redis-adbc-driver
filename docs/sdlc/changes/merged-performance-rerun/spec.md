# Measurement contract

- Pin merged source and build both native library and CLI with an explicit
  unreleased version; verify GetInfo, binary hashes and source SHA-256.
- Use the independently typed 100K-row ×105-column Arrow fixture. Reads cover
  managed SQL, unmanaged SQL and redis-py Search/HMGET, one/four readers and
  512/4096 batch ceilings, six measured repetitions after warmup.
- Writes and creation cover the existing matched 10K-row workloads, one/four
  writers where supported, 1K call/pipeline units and three measured repetitions
  after warmup. Creation retains its different index/consistency semantics.
- Use owned Redis Software, one 4-GiB RAM shard, noeviction, active/effective
  4x QPF, six Search workers, mnp routing/32 connections, verified data TLS/RESP3.
  Verify effective settings before every case. Keep fixture CPU/memory limits
  matched to the historical full matrix.
- Fetch and independently validate complete values, physical types and
  multiplicities before accepting timings. Retain all attempts, including any
  failures; do not replay SQL/Arrow input or publish partial success latencies.
- Publish reviewed sanitized observations and recomputed summaries; remove only
  owned fixture resources. Historical differences are observations across
  revisions and time, not attribution to one PR or confidence intervals.

See [intent](intent.md) and [plan](plan.md).
