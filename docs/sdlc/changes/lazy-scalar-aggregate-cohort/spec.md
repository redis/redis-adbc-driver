# Specification

See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Registration and eligibility

After successful parent SELECT planning, enumerate only already-bound scalar subquery nodes in its output expressions. Do not bind additional statements, validate a lease over the network, fetch data or finalize values. Require at least two compatible siblings and at most 32 eligible siblings in the parent; a cohort may use at most eight user fields. Sources must have identical serialized table descriptors/schema and the exact same statement snapshot scope/store.

Initial members are COUNT(*) and AVG of a bare integer/decimal user column, with no FILTER/DISTINCT/order/group/window/pagination or correlated reference. Predicates must bind through the existing small pure integer/boolean predicate compiler. Reject live/unmanaged/private/retained/temp/foreign/rewriting sources, CTE/derived/view/join/group relations, materialized views and partition scopes. A private in-process partition marker makes the exclusion explicit without changing storage or public protocols. Unsupported members retain their existing execution path; a single eligible sibling is not fused.

## Actual invocation and shared raw scan

Only an actual uncached `subqueryRows` invocation activates a registered cohort. No outer rows, or a never-selected nested CASE arm, produces zero scans. Use the original ascending exact-directory snapshot scanner; do not use indexed candidate shortcuts, server aggregate reducers or positional HASH predicate bypasses. Fetch the union of needed native fields and default/NULL markers. Even a zero-user-field COUNT cohort uses a non-nil empty field list, avoiding HGETALL. Keep row identity/tombstone/cardinality/lease/transport/cancellation checks fail-closed.

Only one bounded raw page is retained. Cache a row's decoded field value/error once, while each lane visits its needed fields in metadata order before evaluating WHERE, including rows that WHERE rejects. The actually invoked member's first decode/evaluation error aborts before later pages, following the serial reference. Record inactive lanes' first errors without surfacing them; continue the physical scan for the active member. Use existing exact accumulators only on matching inputs. No aggregate state retains source rows.

Finalize only the member actually requested, using the same inferred result type, accumulator result method and output expression evaluation as ordinary scalar aggregation. Populate the existing statement cache only for that invoked member. An unused AVG remains unfinalized and uncached. Later actual members reuse complete accumulator state or raise their saved errors. Recheck the source pin before publishing a newly requested lane. Global source/control/cancellation failures cannot become successful cached member results.

## Boundaries

State lives in a fresh per-statement execution cache and is tied to one exact snapshot scope; no cross-statement/cache reuse or detached goroutines are introduced. Parallel workers have their own existing execution caches and do not share this cohort state. Source scanning is serial and deterministic; speculative work on unused lanes can affect CPU cost, but must not surface their SQL/value errors early. This prototype deliberately uses full original-directory control coverage and makes no generic selective-scan performance claim. Root review and an immutable rebuilt artifact are required before a Q9-only live pilot.
