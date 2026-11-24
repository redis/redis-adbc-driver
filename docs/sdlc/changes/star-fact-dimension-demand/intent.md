# Intent: propagate prepared dimension keys to streamed facts

Date: 2026-11-23. Public stack base: `c73e5b66bf45d8c0eb1203171c04b399dbcbc4ca` (#607). Local candidate, unreleased.
Requestor: repository user through parent DS investigation; author: Codex.

The star visitor builds small dimensions but previously scanned facts using only
static source filters. Necessary bounded key sets from prepared dimensions can
narrow native facts and guarded UNION branches before hash-join probes, including
Q5's selective date demand. Preserve complete join semantics and avoid another
full raw-fact scan when supported. No benchmark success or timing claim follows.
See [specification](spec.md), [plan/evidence](plan.md).

Origin: isolated changes `08a26a3c` and shared-IN-list correction `b2cd6495`,
applied without integration ancestors onto the published #607 head. This local
stack contains only prepared-key demand and its hashed-membership correction;
publication is held pending separate root authorization.
