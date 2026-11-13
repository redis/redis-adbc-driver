# Validation — November 13, 2026

## Source and setup

Pinned merged source `ba855f7995b2685182ed1331bcf6703d73089c4a` in a separate
checkout; built library and CLI through
`make -B -C go build adbc-cli VERSION=v0.1.0-dev.merged-perf`.
Go build metadata verified Go 1.27.3; the harness verified GetInfo's version.
The independent typed source SHA-256 matches the historical full comparison.
Library, CLI and unchanged harness hashes are retained in the sanitized results.

Started a fresh owned Software fixture through `make -C go redis-up`, using a
pinned local Software image, private project/network/ports and generated random
credentials. Applied the same four-CPU/eight-GiB container limits and one-shard,
4-GiB database configuration as the previous full matrix. Verified active 4x
QPF, six Search workers, mnp routing/32 connections, noeviction and authenticated,
certificate-verified data TLS/RESP3. Effective configuration was checked before
every case and at completion. The management API retained its bootstrap
self-signed certificate. No other fixture was used or changed.

Three pre-measurement controller corrections handled a null Docker IPAM config,
explicit management-field selection and CLIENT INFO's string protocol value.
All were resolved before the one benchmark invocation; no measured attempt was
restarted, discarded or replayed.

## Actual run and audit

Ran the pinned `benchmarks/table-modes/arrow_compare.py --suite all` with the
verified source/library/CLI, explicit build version and a fresh private work
directory. Default six read and three write/creation repetitions after warmup;
100K read rows, 10K write rows, 105 public columns and 1K call/pipeline units.

- Process exit 0 and final status `complete`.
- All 156 trials retained: 84 reads, 40 existing-table writes, 32 creation.
- Every result independently fetched/validated for physical Arrow types, values,
  counts and multiplicities. Zero failed/conflicted trials or skipped matrix
  cases. No SQL/Arrow caller replay; no partial successful-load latency.
- `python3 benchmarks/table-modes/merged_perf_audit.py` passed complete matrix
  identities/repetitions, source/version/input hash, effective QPF, status,
  validation accounting, timing distributions and worker-RSS arithmetic.
- Historical distributions remain unchanged. The report distinguishes timing
  boundaries, different creation semantics, concurrent managed admission and
  observations across revisions/time from isolated PR effects.

The new audit reuses the established matrix/provenance checker and additionally
checks all-success outcomes and recomputes worker-RSS medians. It was exercised
against the complete fresh data; no new production behavior or test server was
introduced by the report tooling. All result/change-record links, `git diff --check`, sanitized artifact privacy
checks and complete-status assertions passed.

## Ownership and cleanup

After the successful final gate/audit, removed only the owned Compose project
container and network. Before/after inventories confirmed every pre-existing
container and network was preserved. Removed the generated environment file,
TLS key/certificate directory and private generated credential settings.
Raw logs, input data, infrastructure observations and inventories remain private;
public artifacts contain only reviewed synthetic benchmark observations,
software/hardware settings, checksums and recomputed summaries.

No ClickBench, Flex, full-scale loading, Flight SQL, external endpoint or
snapshot-aware NativeClient benchmark was run.

See [intent](intent.md), [specification](spec.md), [plan](plan.md) and
[results](../../../../benchmarks/table-modes/merged-main-2026-11-13/report.md).
