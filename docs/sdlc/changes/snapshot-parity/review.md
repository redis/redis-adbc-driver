# Review: read and shared-partition checkpoint

Date: 2026-11-02. Implementation `9bc80c0`. Author self-review using
[REVIEW.md](../../../../REVIEW.md); this is not maintainer approval.

Correctness/lifecycle: inspected immutable visibility exclusion masks, exact
candidate shadowing, AVG partial merging, DISTINCT global tuple union, lazy
aggregate child cursor cleanup and ordered global OFFSET/LIMIT. Typed exactness
proofs remain the admission gate; large masks/private unsettled data use
bounded authoritative fallbacks. Correlated late-projection dependencies and
materialized point reads had reproduced findings that were corrected and
regression tested. Shared partition admission uses Redis time; producer close
preserves capabilities, release/expiry blocks new children, active readers
retain independent renewable pins. No uncertain SQL/cursor replay was added.

Ownership/security: external data remains managed copies with explicit refresh;
UUID capability validation rejects malformed/tampered roots. Descriptor payloads
remain query data and grant no authentication. Catalog row data still shares the
metadata hash slot. Unrelated missing indexes no longer block statement
acquisition; referenced indexes/directories are verified once per statement.
Only owned private outputs skip immutable-source readiness checks, and final
publication still seals their indexes. Benchmark resets require an explicit
local-fixture acknowledgement; results contain no endpoints or credentials.

Evidence/remaining uncertainty: see [validation](validation.md) for actual
checks, corrected failures and seven-case comparison. No unresolved reproduced
read-milestone finding remains. Full feature/CI, Cluster API recovery reruns,
pre-snapshot performance comparison, metadata sharing/independent publication,
one-build indexes, large atomic operations and sustained compaction remain
required by the active goal. Keep PR #284 draft while this work continues.
