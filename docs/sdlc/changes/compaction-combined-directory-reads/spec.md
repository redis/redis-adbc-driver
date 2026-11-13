# Contract

See [intent](intent.md) and [plan](plan.md).

For larger jobs, seek each retained immutable score-zero row-ID directory with
a lexicographic page query whose bound is the durable checkpoint. Fetch that
page alongside its rank count, preserving the rank-derived complete-directory
cardinality proof and canonical row-ID handling. Bound each pipeline to eight
directory commands and each source page to 64 IDs. Preserve the 64-row/1-MiB
copy checkpoint, all writer fences, source references, input deduplication,
newest-version/tombstone visibility and uncertain-outcome handling.

Small selective jobs retain their serial path. A missing/truncated/malformed
directory or failed rank/page result must fail before advancing the durable
copy checkpoint. No SQL/input or mutation is replayed.
