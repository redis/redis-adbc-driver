# Q19 follow-up after PR #314

Issue #306 was updated with a timeout on merged commit
`baa961075c0577c8fcf66ded1f006616f1ef8491`. Reading the archived
`results/query-19.json` during this investigation confirmed trials 1 and 2
were `QueryDeadline` with null timings, the 62,126-MiB cap, and approximately
15.6/15.9 billion bytes of cgroup peak memory. No completed stage profile was
available. This confirms a remaining full-scale failure; it does not identify
which stage caused either timeout.

## Execution changes

Q19's exact server source-tuple grouping uses `LOAD *` before GROUPBY/TOLIST
checks, because the index can round BIGINT/timestamp values and normalize
strings. On the official schema, that means Redis reads all 105 HASH fields
before grouping three needed source columns. The expression COUNT plan now
uses bounded narrow row scans instead. Exact values and residual predicates
remain in the driver; the shared worker grant and partition boundaries remain
in force.

For groups containing only COUNT(*), spill keys now contain the evaluated SQL
group keys alone. Two different EventTime values with the same minute, UserID
and SearchPhrase can therefore combine their multiplicities inside a sorted
run and across merge passes. Complete source values still provide a valid
representative for projection. Mixed COUNT(column), DISTINCT, SUM/AVG and
other aggregate inputs retain their complete input tuple keys.

The final COUNT(*) top-K merge compares complete counts before decoding source
values. Losing groups do not allocate Value objects, timestamp evaluation,
accumulators or representative maps. ORDER BY on group values retains the
established reducer. HAVING is checked after the full group count and before
top-K. OFFSET+LIMIT is retained, and unordered LIMIT can stop only after
complete counts have been merged across all input runs.

## Reproducible local comparison

```sh
cd go
REDIS_URI=redis://localhost:6379/0 REDIS_Q19_PROFILE=1 \
  REDIS_Q19_PROFILE_ROWS=100000 go test -count=1 -v -run '^TestQ19Profile$' .
```

The test creates/drops only `it_q19_profile`, a synthetic 105-column table with
100,000 rows. It uses BIGINTs beyond 2^53, varying timestamps, and string groups,
plus unrelated payload fields. It compares three trials per strategy on the
same indexed data with RESP3, 65,536-ID ranges and one partition caller. The
baseline reproduces PR #314's source-tuple keys and general reducer; the
candidate combines narrow scans, SQL COUNT group keys and count-only top-K.
All six executions returned identical selected groups and complete counts.
The fixture's deterministic group-key merge order handles COUNT ties equally
in both strategies; official SQL permits other tied groups.

Medians from [profile.json](profile.json):

| Metric | Source tuples | Narrow COUNT groups | Reduction |
|---|---:|---:|---:|
| Partition work | 2.570 s | 2.192 s | 14.7% |
| Final merge | 0.276 s | 0.054 s | 80.3% |
| Total allocated bytes | 1,434,645,696 | 734,792,256 | 48.8% |
| Redis socket read bytes | 37,483,524 | 19,809,063 | 47.2% |
| Spill bytes written | 12,506,722 | 10,806,722 | 13.6% |

Allocations are cumulative Go allocations, not peak RSS. Socket bytes include
reply framing and pipelined HASH reads; they are measured client reads, not
Redis CPU. Partition and merge timings are sequential local wall times.
This is a synthetic single-node pilot and establishes neither the cloud
bottleneck nor completion on 99,997,497 rows within 300 seconds.

## Diagnostics that survive a deadline

The trial writes an atomic `qNN-T.progress.json` sidecar before connecting and
at each stage transition, and refreshes it every five seconds while running.
It records completed stage times, the active stage's elapsed time, process
CPU and peak RSS. The sampler does not access the ADBC connection during
execution. Progress I/O failures cannot replace query errors. The supervisor
retains the sidecar as `partial_diagnostics` for failed trials while preserving
null query timings and the original QueryDeadline/MemoryLimit outcome.
No URI, SQL output values or success timing is written to this sidecar. A
hard-killed mock child regression verifies its execute checkpoint survives.
These samples are incomplete lower bounds at the last write, not completed
execute/fetch timings or actual Redis work/worker-grant metrics.

## Full-scale acceptance still required

Keep #306 open. Rerun the official SQL three times on the same verified dataset,
runner placement, exact mode, 62,126-MiB process cap and 300-second whole-trial
deadline. Preserve old trials, record tested commit/binary SHA256, and compare
completed output with the existing typed DuckDB reference. For different
COUNT ties, verify each returned group's exact count and the ordering cutoff
rather than demanding identical arbitrary groups. Capture checkpoints,
actual worker grants, aggregate metrics, Redis FT.PROFILE/server work, wire
bytes, CPU, spill I/O and peak memory. No live cloud runner or sweep was
modified by this source change.
