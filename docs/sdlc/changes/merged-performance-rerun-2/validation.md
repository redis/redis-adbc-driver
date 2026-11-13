# Validation — November 13, 2026

## Source and fixture

Pinned clean source `5f759300c12f5191a36d2f2b5a29555ef14ef486` in an isolated
checkout. Built library and CLI using `make -B -C go build adbc-cli
VERSION=v0.1.0-dev.merged-perf-2`; verified Go 1.27.3, exact VCS revision and
`vcs.modified=false` before measurement. The harness checked GetInfo's explicit
version. The typed input SHA-256 matches the previous full comparison; all
source/library/CLI/harness checksums are retained in sanitized observations.

Provisioned owned Redis Software through `make -C go redis-up`, using a pinned
local Software image, private isolated project/network/ports and generated
credentials. Verified effective four-CPU/eight-GiB container limits, one 4-GiB
RAM shard, noeviction, active 4x QPF, six Search workers and mnp/32 connections.
Data TLS certificate/hostname verification and RESP3 were required. Effective
QPF was checked before every case and after the final case. The management API
retained its fixture bootstrap self-signed certificate.

Private controller initialization needed one correction for null Docker IPAM
configuration before the actual build/provisioning or benchmark invocation.
No measured attempt was restarted, discarded or replayed. No other heavy task
was started by this chat during measurement; this remains a shared workstation.

## Outcomes and audit

The unchanged pinned `arrow_compare.py --suite all` completed once, exit 0,
status `complete`. Every one of 156 trials, including labeled warmups, passed
independent complete physical-type/value/count/multiplicity validation:

- 84 reads: 100K rows × 105 columns, one/four readers, 512/4096 batch ceilings,
  six measured repetitions after warmup for each case.
- 40 existing-table writes: 10K rows, one/four writers and 1K SQL/Arrow/HSET call
  units, three measured repetitions after warmup.
- 32 creation workloads with the established differences in index and
  publication semantics; no partial successful-load timings.
- Zero failed/conflicted trials, skipped matrix cases or caller retries.

`python3 benchmarks/table-modes/merged_perf_rerun_audit.py` passed exact source,
version/input hash, all case/repetition identities, effective QPF, complete
validation and elapsed/worker-RSS arithmetic. The previous full-run audit also
passed after its summarizer accepted optional source/version arguments; defaults
and historical observations remain unchanged. No production driver code changed
in this evidence branch.

The report retains all samples, timing boundaries, first-batch/RSS distributions
and cross-revision/time limitations. The observed one-reader unmanaged change
was 17.8% less median elapsed time and 41.3% less maximum-worker RSS versus the
previous full run. Filtered four-reader queries retain their existing path.
No rejected reply-budget or early-index prototype was enabled.

Reviewed public artifact privacy and checked every local Markdown link in the
new evidence/change records and table-mode index. Both passed. Repository
whitespace checks passed before commit; raw operational artifacts remain outside
the repository. These checks do not introduce additional timed trials.

## Ownership and cleanup

Removed only the owned Compose project container/network after successful final
gates and audit. Before/after inventories confirmed every pre-existing container
and network was preserved. Verified removal of the generated environment, TLS
key/certificate directory and private generated credential settings. Private
logs were checked for generated credential values before settings removal.
Operational logs, infrastructure identifiers and raw input remain private.

No ClickBench, Flex, full-scale loading, alternate provider or snapshot-aware
NativeClient benchmark was run.

See [intent](intent.md), [contract](spec.md), [plan](plan.md) and the
[complete report](../../../../benchmarks/table-modes/merged-main-2026-11-13-rerun/report.md).
