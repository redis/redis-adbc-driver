# Execution plan

1. Inspect release/bind/device paths and pinned Arrow import/export implementation.
2. Add the gated fixture and C-ABI runner under `benchmarks/ffi-release-gc/`.
   Keep the omit-GC patch experimental; freeze hashes before any measurements.
3. In an exclusive slot, build both fixture libraries before timing, run ownership
   checks (including a race build if supported), then matched per-release samples.
4. After a separate loan, verify the Software fixture immediately before unchanged
   native full-read controls and return the owned database empty/index-free.
5. Audit all samples/provenance, disclose sanitizer/platform limits and negatives,
   and request root review before any publication or production adoption.

See [intent](intent.md), [specification](spec.md), [validation](validation.md).

Execution complete: all 56 C-ABI and 56 native observations retained; omission
rejected for default adoption. Both race gates blocked and ASAN unrun. Final
parent teardown proof and agent review are complete; publication is complete; human review remains pending.

Published for review in [PR #425](https://github.com/redis/redis-adbc-driver/pull/425);
not merged.
