# PostgreSQL 18 query semantics: specification

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `85509485`. Status: targeted join/whole-row slice implemented and validated; five #361 cases remain.

Parenthesized joins must preserve join association, public output order, column aliases, qualified names and NULL semantics. A whole-join alias hides the contained names; USING aliases expose only merged columns without adding duplicate unqualified columns. Whole-row references must preserve field names, exact field types and the distinction between a missing outer-join row and a present all-NULL row. Composite comparison must independently validate PostgreSQL values and physical Arrow fields. Deferred #361 cases stay visible until observed passing.

Run Redis tests only through verified Redis Software with active 4x QPF. Keep endpoints, credentials and operational logs private. See [intent](intent.md) and [plan](plan.md).
