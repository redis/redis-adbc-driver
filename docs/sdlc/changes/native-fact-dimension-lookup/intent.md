# Intent: carry bounded dimensions into native lookup facts

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: local candidate, unreleased.
Requestor: repository user through parent DS investigation; author: Codex.

The prior planner only delayed deferred UNION facts for small disconnected
inputs; a large native fact could be scanned before its selective date dimension.
Native lookup then stopped after its first eligible equality pair, omitting a
second available date/store condition. Extend these existing bounded operations
without a broad Cartesian rule or a new large-integer Search precision proof.
Origin: isolated committed change `7a51d7c595b11a97f22fbafe59fc602717148104`,
plus its isolated native-only planning follow-up
`0bed7ce5112ea6d4ca02b829905f6564d8aeabbe`, applied without integration ancestors. See [specification](spec.md),
[plan/evidence](plan.md). Publication is held for the next targeted Q8 outcome.

The measured Q8 candidate set depends on the larger bounded complete-candidate
cap in [draft608](https://github.com/redis/redis-adbc-driver/pull/608). The join
optimization remains exact and independently testable on this public baseline;
no standalone native SF1 success or timing claim is made.
