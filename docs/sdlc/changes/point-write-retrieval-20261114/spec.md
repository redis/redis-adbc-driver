# Contract

[Intent](intent.md), [plan](plan.md). Date: November 14, 2026.

Preserve protocol 17, exact int64/string/NULL behavior, physical namespace and
slot routing, snapshot pin/lease proofs, newest-version/tombstone visibility,
per-statement native permissions, live source membership/existence/wrongtype
policy, cursor/Arrow ownership and cancellation. Batch independent proofs only
when original error precedence and unknown outcomes are retained. Native
permission/NOSCRIPT refusals may only select proved non-execution compatibility
paths; never replay an unknown row/cursor/write outcome. No TTL or live-value cache.

Use Redis Software with active/effective 4× QPF, six workers, mnp/32, noeviction, strict
data TLS/RESP3 and actual resource records. Coordinate one live slot; no heavy
Go tests/builds during native performance windows. Retain every raw sample and
failed attempt. Run final baseline/candidate matrices consecutively after local
checks to reduce the previous ordinary-suite-between-matrices confound. Report
flat/slower cases, overhead/guarantee differences and finite-sample limits.
