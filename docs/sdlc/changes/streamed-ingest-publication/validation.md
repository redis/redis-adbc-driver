# Validation

November 13, 2026. Existing-API comparison and final shared infrastructure
teardown completed. Driver Go code and
dependencies are unchanged from the clean 24f654b7 baseline.

The same-source full reference also passed all 156 trials (84 reads, 40 writes,
32 creation), with zero failures/conflicts/retries/discards. The separate
current_main_next_audit.py verifies all cells and distributions; its full
measurements/provenance report is linked in the benchmark README.

All 24 native trials validated all 105 physical types/values/counts/multiplicities
and the exact 10K ordinal set. Each of six cases retained its warmup plus three
measured trials; no conflicts/retries/failures/discards. The audit independently
recomputes every distribution, exact public call and input batch counts,
source/version/protocol/QPF metadata and completion state.

The source-only subagent review found no blocker in frozen helper 4491ae27.
It checked synchronous RecordBatchReader consumption/close, acknowledged counts,
disjoint caller ranges, setup versus timing/validation boundaries and no replay.
This is agent review, not human maintainer approval. No error-injection or new
Go integration-suite result is claimed by a benchmark/documentation-only change.

Syntax, arithmetic audit, relative file links, scoped artifact privacy and
`git diff --check` passed. Complete [results and limitations](../../../../benchmarks/table-modes/streamed-publication-2026-11-13/report.md)
retain observed memory tradeoffs and the intentionally different publication
units. The usage guide clarifies managed whole-operation publication versus
unmanaged acknowledged partial effects; source inspection of executeIngest
confirmed the distinction before documenting it.

Shared provisioning had two controller-only corrections before tests/timing:
Docker network IPAM Config can be null, and API polling projections omit uid.
The controller now accepts empty network configurations and retains the selected
database identity outside polling. Effective fixture assertions were not weakened;
the retained private attempt records distinguish these from measurement results.

The owner verified Redis Software 8.2.6-25, active/effective 4x QPF, six workers,
mnp32, one 4-GiB noeviction shard, strict data TLS/RESP3 and actual four-CPU/
eight-GiB container limits before execution. The tag was not digest-pinned; the
actual local container image identity is recorded privately and unchanged.
The loan returned empty/index-free with clients/processes closed and effective
configuration reverified. Root completed final container/network/TLS/credential cleanup after all sibling
comparisons: owned resources removed, pre-existing container and 31 networks preserved,
generated environment/settings/fixture-env secrets and CA files removed.
[Sanitized cleanup proof](../../../../benchmarks/table-modes/current-main-next-2026-11-13/cleanup.json)
is retained; raw operational artifacts stay private. Zero credential matches
required redaction, leaving raw digests intact. No ClickBench was run.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
