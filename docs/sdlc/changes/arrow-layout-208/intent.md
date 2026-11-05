# Layout assessment closeout

Date: 2026-10-31. Requestor: repository user in the active Codex chat.
Base: `198de097e88bc48adc99b9dcdbe5ac4fa8341c97` (merged PR #266).
Issue: [#208](https://github.com/redis/redis-adbc-driver/issues/208).

Complete the assessment's decisions and validation rather than treating Arrow
library availability as driver support. Establish exact normalization/rejection
behavior for FixedSizeList, both list views, run-end encoding, Map and both
Unions, with C bindings, nested/sliced/empty/NULL data and independent replay.
Bound decoded expansion before materializing compressed/shared inputs.

True Map and Union storage remain separate implementation features tracked in
[#267](https://github.com/redis/redis-adbc-driver/issues/267) and
[#268](https://github.com/redis/redis-adbc-driver/issues/268), both enhancement
issues. No extension registration, general blocking-operator memory guarantee,
transport fix or performance improvement is claimed.

See the [specification](spec.md) and [plan/evidence](plan.md).
