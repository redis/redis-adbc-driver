# Specification: current performance evidence for the slides

See [intent](intent.md) and [plan](plan.md).

- Build the Linux native library and CLI once from clean main `34f4c829`; use the same exact library/version/source for both benchmark matrices. Keep `adbc.redis.writer_admission_queue` at its default false.
- Use `make -C go redis-up` on an exclusively owned Redis Software database and verify active/effective 4x QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard, trusted data TLS/RESP3, and server/client resource limits before measurement.
- Retain the complete 156-trial bulk matrix (84 reads, 40 existing-table writes, 32 creation) and 168-trial scenario matrix (96 OLTP, 72 OLAP), including all warmups, slower cases and any failures. Do not retry SQL/input or report incomplete results as throughput.
- Independently validate complete physical types, values, row counts, multiplicity and point-mutation final state using existing harness oracles. Preserve exact timing boundaries and distinguish cold worker startup from persistent-client operation latency.
- Generate chart JSON/CSV through the existing strict complete-matrix exporter. Preserve units, mode, concurrency, samples, fixture dimensions and redis-py elapsed multiples. Keep historical ClickBench and aggregate data separate.
- Publish sanitized evidence and reproducible offline audits; keep endpoints, owner identifiers, credentials, certificates, license material, and raw operational logs private. Remove owned resources and generated credential material after clients exit.
- Update the numeric performance slides using only validated current results; retain historical product comparisons with their original labels. Open a reviewable PR containing evidence and limitations.
