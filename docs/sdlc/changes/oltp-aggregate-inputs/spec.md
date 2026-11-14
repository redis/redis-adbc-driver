# Specification

[Intent](intent.md), [plan](plan.md). Author: Codex. Human review pending.

Only immutable managed BIGINT columns may enter indexAggregate's established
runtime exactness checks in default exact mode. Every group/MIN/MAX value must
be strictly inside (-2^53,2^53); SUM additionally requires non-NULL count times
the largest input magnitude strictly below 2^53. Boundary rounding, mixed-sign
cancellation, unsupported values and precision uncertainty retain exact HASH
fallback before returning output. Generic pushable/groupable eligibility stays
unchanged; live application indexes receive no new reducer permission. Existing
snapshot masks, residual/cursor guards, NULLs and output casts remain intact. A
typed Search group cap declines only this optional new BIGINT optimization,
retaining the established supported driver fallback instead of adding a new
resource error for casted outputs outside the bounded tuple-plan subset.

Direct column group keys and COUNT/SUM/AVG/MIN/MAX inputs may use one ephemeral
Value vector per scan. Managed rows retain existing authoritative visibility;
unmanaged rows retain key/existence/membership checks and live HASH residual
validation. COUNT(*) uses an application key for unmanaged rows, never an
internal ID. Expression, FILTER, DISTINCT, ordered-set and unsupported predicate
shapes retain established whole-page evaluation/error order. Representative maps
retain only group keys and established dependencies, not every input row.

Review: [PR #437](https://github.com/redis/redis-adbc-driver/pull/437). Human maintainer approval remains pending.
