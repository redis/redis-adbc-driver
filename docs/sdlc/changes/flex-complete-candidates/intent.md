# Intent: complete bounded Flex candidates

Date: 2026-11-23. Base: `07b24edb`. Status: local candidate, unreleased.
Requestor: repository user through parent benchmark investigation. Author: Codex.

The prior page-filter candidate still timed out on the live TPC-DS Q3 retest.
Enumerating every row directory and proving/searching each 256-key page does not
solve selective scans. Retain immutable Search predicate candidates only when
their complete materialization fits explicit result and memory bounds; resolve
visibility through exact directories, as the existing RAM candidate path does.

No benchmark SQL, snapshot format, full benchmark restart, or native SORTBY,
cursor, floating row-ID filter, or deep OFFSET is introduced. This candidate
requires independent review and live query validation before a fix claim.
See [specification](spec.md) and [plan/evidence](plan.md).

Publication is reconstructed directly on public main baseline `07b24edb`, containing only Flex scan/candidate code and supporting records. CTE/join/view/UNION changes from the separately tested combined artifact are excluded. Related: [Q3 #589](https://github.com/redis/redis-adbc-driver/issues/589), [Q2 #588](https://github.com/redis/redis-adbc-driver/issues/588), [Q4 #590](https://github.com/redis/redis-adbc-driver/issues/590), [Q5 #591](https://github.com/redis/redis-adbc-driver/issues/591), [Q9 #595](https://github.com/redis/redis-adbc-driver/issues/595). No issue closure or pure-Flex SF1 performance claim follows. See [validation and evidence](validation.md).

Reviewable Flex-only implementation: [PR #604](https://github.com/redis/redis-adbc-driver/pull/604).
