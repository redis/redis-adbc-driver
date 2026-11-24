# Specification

Date: 2026-11-23. Status: local candidate.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

Reuse the complete-candidate pinned immutable managed plain-index eligibility
gate. Before admitting a compiled selective query to parallel directory reads,
validate the lease and every source's fresh identity/readiness-bracketed Search
COUNT0 response. Reject warnings, malformed/negative counts, nonempty keys in a
count reply, counts above physical source rows, wrong index identity and all
ambiguous/transport/ACL errors. Validate every later source even after an early
source exceeds a budget, then validate the lease again before returning proof.

Factor count parsing and the exact existing complete-candidate admission budget
into shared helpers. Both paths use the same installed count ceiling and independent 16 MiB estimated key-byte cap across all sources. This standalone branch retains 65,536 candidates; companion [PR608](https://github.com/redis/redis-adbc-driver/pull/608) changes that constant separately. The proof follows whichever constant is installed, without a second hardcoded threshold. Saturate
planning totals on refusal to prevent integer overflow. A native result-limit
capability refusal alone does not prove over-budget planning and retains the
existing path; unsupported gates and admitted selective counts also stay serial.

Only a successful count/byte-budget proof expands parallel fact admission.
Preserve the entire typed fact predicate, including dynamically prepared key
constraints, as original/residual SQL. Force worker Search query to `*` and use
only existing exact int64 directory range scans. Workers do not enumerate or
fetch predicate candidates. Keep original HASH fields, visibility/tombstone
rules, lease checks, worker-budget ownership, bounded queues and the single exact
reducer. No source-error fallback or midstream replay is introduced.
