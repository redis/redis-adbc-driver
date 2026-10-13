# Partitioned exact aggregates

Issues #293–#303 report failures on the 99,997,497-row ClickBench dataset:
Q5/Q9/Q11/Q12/Q14 exceed Redis Cloud's aggregate group cap, and
Q8/Q10/Q13/Q15/Q16/Q17 exceed the 300-second isolated-trial deadline.
A cursor limits returned pages, but does not limit the groups Redis builds.
The previous parallel row reducer also excludes grouped queries.

## Execution

Supported simple column aggregates on owned tables with at least 65,536
allocated row IDs use disjoint 65,536-ID query partitions. The query's index
predicate is intersected with each range. Each worker groups complete input
tuples on Redis, checks canonical representation, NULL presence and complete
TOLIST equality, and receives tuple multiplicities. SQL residual predicates
are rechecked on the exact tuple. This includes the eleven reported query
shapes, with COUNT, COUNT DISTINCT and mixed integer SUM/AVG.

A group-limit failure retries finite ranges by bisecting them. Single-ID
ranges and the open-ended final range use the row scan when necessary.
Collision checks and noncanonical values also select the exact row scan for
that partition. Attempts must finish and validate before any result is
accepted, so a failure on a later cursor page cannot duplicate partial counts.
Small supported queries that hit the group cap enter this strategy on demand.
Authentication, cancellation, socket errors and unrelated resource failures
remain errors. Unsupported SQL retains its existing strategy; an unsupported
server group-limit failure remains visible.

Workers sort exact, length-delimited tuple keys into private temporary files.
Each buffer flushes at approximately 8 MiB or 65,536 tuples, whichever comes
first. One oversized tuple can exceed the byte target. Merge passes open at
most 32 runs, combine multiplicities, and reduce consecutive SQL groups.
COUNT DISTINCT arguments precede other inputs in the sort key: equal distinct
tuples are consecutive even when SUM/AVG inputs vary. No cardinality-sized
client distinct set is needed. Integer multiplication and accumulation use
arbitrary precision; AVG keeps its non-NULL weighted sum and count. No values
are converted to double to establish BIGINT equality.

ORDER BY/LIMIT operates on final counts. Simple ordered queries retain only
OFFSET + LIMIT final groups. Queries returning all groups still require memory
for those output groups. Ties without a complete SQL tie-breaker may differ
from another execution or reference; they are not evidence of wrong counts.

This is a bound on retained tuple state, not a hard process memory limit:
each worker also holds one server partition, cursor replies and decode buffers.
The final range includes concurrent inserts; if it becomes too large for the
server, its row scan still spills incrementally. Arbitrarily large field values
can exceed nominal buffer sizes. The benchmark's cgroup limit remains in force.
Temporary storage grows with tuple cardinality and may require multiple merge
passes. Set TMPDIR to a volume with adequate free space; disk errors fail the
query. Query-local files, cursors and worker grants are cleaned up on return or
cancellation. A process killed without cleanup can leave a temporary directory.
Adopted tables, multiple different DISTINCT argument tuples, aggregate FILTER,
complex grouping, HAVING and windows keep their established paths.

## Diagnostics and validation

The read-only connection option `adbc.redis.aggregate_metrics` returns JSON for
the last partitioned query, or `{}` for another strategy. It includes actual
workers, partition attempts, retries, scan fallbacks, returned tuples,
represented rows, logical value bytes, Redis reply time, worker partition time,
merge wall time and written spill bytes. Returned tuples/bytes include failed
or discarded attempts when those replies are available. Logical bytes are not
RESP wire bytes. Redis reply and partition times are summed across workers;
they are neither wall time nor measured Redis CPU. Spill bytes include
intermediate merge output.

The ClickBench trial records connection, session setup, execution, Arrow fetch
and output/fingerprint stages, actual worker grants, aggregate metrics, process
CPU and peak RSS. Whole-trial cgroup peak memory and deadline outcomes remain
in the supervisor. Diagnostics cannot mask an execution failure. Use the
existing benchproxy wire counters and Redis FT.PROFILE/server measurements
separately when attributing network and server CPU work.

Regressions cover the eleven shapes, exact BIGINT and composite/string equality,
NULL-only groups, duplicates across ID ranges/shards, mixed SUM/AVG, cap recovery
at open and on a later cursor page, final top-K with OFFSET, worker grants,
cancellation cleanup and multipass spill merging. Redis OSS lacks the Cloud
MAX_AGGREGATE_GROUPS configuration: the cap fixture wraps real Redis pipelines
and injects the documented Cloud error above a configured tuple cardinality.
It does not prove the Cloud service's performance.

Before closing #293–#303, rerun each query three times on the original full
dataset and dedicated runner, in exact mode with the same 300-second deadline
and memory limit. Preserve old failed trials and write a separate result set.
Record the new binary checksum, stage metrics, wire/server measurements and
independent DuckDB value comparisons, accounting for legitimate top-K ties.
Local correctness and bounded-state regressions do not establish completion
within the full-scale deadline. The original cloud sweep was still active
when these changes were validated locally; it was left running.
