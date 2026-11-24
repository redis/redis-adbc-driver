# Specification

Only scalar aggregate input requests may opt managed rows into the existing pure integer/boolean bound residual evaluator. Unsupported expressions, volatility, subqueries, retained execution, missing bindings, late projection, ordering and other established exclusions retain the generic path. Required fields decode in metadata order before the predicate; NULL and exact int64 comparisons retain existing semantics. The original scan owns visibility, authoritative HASH reads, source identity and cancellation.

Filtered inputs must use keep() to evaluate their predicate before the scalar callback. Neither hashExclusive nor a positional hashFetch consumer may bypass it. Ordinary managed projection behavior is unchanged. No result cache, index reducer, altered snapshot proof, SQL rewrite or increased statement deadline is introduced.

See [intent](intent.md) and [plan](plan.md).
