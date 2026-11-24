# Plan

See [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Add bounded registration-time binding with atomic publication after all lanes validate.
2. Replace row-loop lane.need lookups, predicate field-name searches and AVG input-name searches with the proven indices.
3. Compare values/types/errors with generic decodeRow and predicate evaluation, including rejected-row errors, defaults, NULL and integers beyond 2^53. Assert metadata/predicate ordering and COUNT's unused input slot.
4. Verify missing/duplicate bindings fall back without starting scans or partially changing an earlier lane.
5. Run focused and all unit-only packages and obtain read-only review. Do not build, query a live fixture, mutate the frozen publication worktree or push this follow-up.
