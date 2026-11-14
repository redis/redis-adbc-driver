# Fetch page sizing: evidence only

Keep the 2048-row candidate **unapplied**. Halving the existing 4096-row
positional HASH discovery/fetch ceiling reduced several peak worker RSS medians,
but one-reader managed scans were 26.8–54.7% slower. The original gate required
no clear >3% median elapsed regression outside control variation. This fails it;
no constant/eligibility tuning or selective rerun followed timing. Production
Go files and dependencies on this evidence branch are identical to `88774f10`.
The exact measured change is retained in [candidate.patch](candidate.patch).

## Scope and complete native results

All **168 trials** validated their full projected physical types, values,
100K row counts and multiplicities: 84 per binary, twelve cells each with one
warmup plus six measured repetitions. Wide 105-column reads cover managed and
unmanaged, one/four readers and public output batches 512/4096. Narrow eight-column
reads cover managed with those same reader/batch combinations. No failed trial,
retry or discarded sample occurred. Every observation, including warmups, is in
[native-results.json](native-results.json); all elapsed, first-batch and RSS
samples/ranges are in [summary.json](summary.json).

Before any timing, source review found the ceiling also affects eligible simple
narrow snapshot projections. The eight-column control was added to the frozen
plan before tests/builds/measurements, and caught the largest elapsed regressions.
Generic narrow Search pipelineChunk and multi-segment snapshotDeltaPage remain
unchanged. The candidate changes only wideFetchChunk from 4096 to 2048 plus a
comment clarifying its existing scope; one-page prefetch, visibility/lineage/ACL
proofs, deletion markers, input decoding and cancellation/cleanup are unchanged.

|Columns|Mode|Readers|Output batch|Baseline s|Candidate s|Elapsed change|Baseline RSS MiB|Candidate RSS MiB|RSS change|
|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|
|8|managed|1|512|1.2197|1.8391|+50.8%|205.23|199.88|-2.6%|
|8|managed|1|4096|1.1757|1.8190|+54.7%|206.29|204.61|-0.8%|
|8|managed|4|512|0.8530|0.8767|+2.8%|205.70|200.06|-2.7%|
|8|managed|4|4096|0.7953|0.8595|+8.1%|207.28|203.70|-1.7%|
|105|managed|1|512|2.2373|2.8378|+26.8%|250.99|224.65|-10.5%|
|105|managed|1|4096|2.2429|2.9653|+32.2%|254.29|230.09|-9.5%|
|105|managed|4|512|1.6302|1.5473|-5.1%|252.19|224.27|-11.1%|
|105|managed|4|4096|1.5603|1.5564|-0.2%|256.16|232.76|-9.1%|
|105|unmanaged|1|512|5.2685|5.1851|-1.6%|256.30|255.66|-0.2%|
|105|unmanaged|1|4096|5.0349|5.0223|-0.3%|262.72|262.32|-0.2%|
|105|unmanaged|4|512|1.9312|1.9427|+0.6%|264.13|229.81|-13.0%|
|105|unmanaged|4|4096|1.7514|1.8310|+4.5%|265.99|236.94|-10.9%|

Managed one-reader wide RSS fell 9.5–10.5%, but elapsed rose 26.8–32.2%.
Managed one-reader narrow elapsed rose 50.8–54.7%, for 0.8–2.6% lower RSS.
Managed four-reader wide elapsed ranged from −5.1% to −0.2%, with 9.1–11.1%
lower RSS. Unmanaged one-reader results were nearly flat; four-reader/4096 was
4.5% slower despite 10.9% lower RSS. This is a real tradeoff in this workload,
not evidence for a universally better default. RSS is the maximum individual
worker's peak, including Python/Arrow/the driver, not total workers or Go heap.

## First-batch medians

These include spawned-worker startup and source/schema loading before the first
IPC batch. They are not isolated Redis request latency. Complete distributions
are retained; they do not replace whole-fetch measurements.

|Columns|Mode|Readers|Output batch|Baseline s|Candidate s|Change|
|---:|---|---:|---:|---:|---:|---:|
|8|managed|1|512|0.3005|0.2951|-1.8%|
|8|managed|1|4096|0.3129|0.3290|+5.2%|
|8|managed|4|512|0.3503|0.3251|-7.2%|
|8|managed|4|4096|0.3564|0.3487|-2.2%|
|105|managed|1|512|0.3646|0.3290|-9.8%|
|105|managed|1|4096|0.3695|0.3825|+3.5%|
|105|managed|4|512|0.5177|0.4044|-21.9%|
|105|managed|4|4096|0.5225|0.5057|-3.2%|
|105|unmanaged|1|512|0.3926|0.3738|-4.8%|
|105|unmanaged|1|4096|0.5611|0.5642|+0.6%|
|105|unmanaged|4|512|0.5113|0.4124|-19.3%|
|105|unmanaged|4|4096|0.4855|0.5227|+7.7%|

## Controls, correctness and limitations

Both binaries use the exact same [frozen helper](../fetch_sizing_compare.py)
and unchanged run.py read workers. Every fixture loads the complete 100K×105
source and full field indexes. Only the selected SQL/output schema changes for
the narrow control. A spawned process temporarily projects its source schema,
invokes the original read worker and restores its binding in finally. Four
readers process eight disjoint ordinal ranges. Canonical complete-field validation
occurs outside elapsed timing; native input is NULL-free. Source loading/index
readiness/preflight and ownership-checked final cleanup are outside trial clocks.
Worker startup/imports, schema source load, SQL fetch, IPC writing and teardown
are inside. Input materialization and IPC costs prevent treating this as pure
server or conversion throughput.

Focused endpoint-free unit and race runs each passed 16 top-level / 37 including
subtests, zero failures/skips. Verified Software passed eight top-level / 16
including subtests, zero failures/skips: wide both APIs, narrow projection,
NULL/default/exact-integer and missing-row behavior, snapshot visibility,
retained Arrow buffers, producer EOF/drop, live update/delete and cancellation.
Existing tests exercise dynamic page boundaries; no assertions/skips were weakened.
[checks.json](checks.json) retains case names, outcomes and private raw-log hashes.
This is selected coverage, not the entire repository suite. Both Makefile-built
library/CLI pairs have Go 1.27.3, exact clean embedded source and vcs.modified=false.

Baseline was one complete block before candidate; case order reverses between
repetitions. This is one shared-host local strict-TLS experiment, without simulated
or remote network latency. Differences cannot all be attributed causally to the
constant. Smaller pages increase discovery/pipeline interactions; this experiment
does not isolate their server/transport contributions or establish that the
compaction reply wait has the same cause. No post-hoc mode/width default is adopted.

## Environment, reproduction and cleanup

Baseline `88774f10cf16fc4260e6ad5077097f4de8828406`, version
`v0.1.0-dev.allocation-base`; candidate `faa05f96158f92c7cb2c18f69addfb4a07202825`,
version `v0.1.0-dev.fetch-2048`. [provenance.json](provenance.json) retains source,
binary, source-data, helper, controller and raw-log hashes. The original candidate
branch remains codex/fetch-prefetch-sizing. See [environment.json](environment.json):
Software API 8.2.6-25 / Redis 8.8.0 / Search 80808, active/effective 4x QPF,
six workers, mnp/32 connections, one 4-GiB noeviction shard, strict data TLS
certificate/hostname verification and RESP3. Owned container limits: four CPUs /
eight GiB. One explicit local image identity stayed fixed without tag resolution;
its operational identity remains private. Host Apple M5 Max, 18 logical CPUs,
36 GiB, macOS 26.7.3 arm64. Native GOMAXPROCS/GOFLAGS/GOWORK are removed;
GOGC/GOMEMLIMIT/GODEBUG were unset. Tests/builds use GOMAXPROCS=4.
Management bootstrap uses its separate self-signed trust mode.

For a new experiment, provision an owned Software fixture via make -C go redis-up,
verify effective settings and privately set REDIS_BENCH_* / SSL_CERT_FILE. Apply
the exact candidate patch to an isolated baseline, use versioned Makefile builds
and run the same helper with --suite reads --read-repeats 6 and fresh private
--work directories for each binary. Record actual versions/settings/hashes and
retain every attempt. The helper provisions nothing and checks exact ownership
before clearing its dedicated database. No ClickBench was run.

Two independent agents and the parent source-reviewed the candidate/helper before
measurement; no blockers found. Agent review is not human maintainer approval.
Every loan ended empty/index-free with all clients/processes closed and QPF/TLS/
RESP3 reverified. Final root teardown removed the owned container/network and
generated TLS/environment/settings/fixture-env credentials, preserving the prior
container and all 31 prior networks; [cleanup.json](cleanup.json) records this.
Generated-credential scans found zero matches; raw measurement hashes are unchanged.
Operational artifacts, identifiers, credentials and license material stay private.

Audit without Redis or Go:

```sh
python3 benchmarks/table-modes/fetch_sizing_audit.py
```
