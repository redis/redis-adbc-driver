# Specification

2026-11-23; see [intent](intent.md), [plan](plan.md). Extract implied conjuncts by intersecting OR branches and combining AND branches, without DNF expansion or shared-AST mutation. Keep the complete original predicate as a residual. Preserve common OR subexpressions such as the parser expansion of IN lists.

Restrict extraction to entire OR expressions made of direct local-column numeric/text comparisons and checked literal-only arithmetic. Reject functions, casts, parameters, subqueries, unsupported types/collations and error-producing arithmetic. Existing nullable and isolated join-boundary checks remain authoritative. The common Q19 equality must reach existing hash-key discovery and both shipping predicates must reach the lineitem scan.

Acceptance: pre-fix structural regression fails; post-fix structural, safety, NULL/duplicate and tiny live decimal-revenue cases pass. Do not claim a full-suite score or an SF1 speedup from small fixtures.
