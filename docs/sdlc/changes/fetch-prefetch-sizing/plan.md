# Plan

1. Freeze intent/spec, exact one-constant candidate and relevant regression set.
2. Provision owned Redis Software using make redis-up; verify active/effective
   4x QPF, six workers, mnp32, noeviction, strict TLS/RESP3 and resource limits.
3. Serialize focused correctness, race, build and baseline/candidate native work
   across sibling experiments. Record clean source/binary hashes and versions.
4. Audit complete samples, compare predeclared gate, source-review the outcome,
   publish accepted code or evidence-only PR with standing user authorization.
5. Return database empty/index-free, join all clients; root removes owned fixture
   and generated TLS/credentials while preserving pre-existing resources.

See [intent](intent.md) and [contract](spec.md).

Review: [PR #432](https://github.com/redis/redis-adbc-driver/pull/432) is open and unmerged; no human approval is claimed.
