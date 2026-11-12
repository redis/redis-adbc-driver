# Specification: reversible asset names

Date: 2026-11-12. Author: Codex; requestor: repository user. Base: `8d4dcacd`.
Issue: [#389](https://github.com/redis/redis-adbc-driver/issues/389). Status: implemented and verified; awaiting human review.

See [intent](intent.md) and [plan](plan.md).

Keep a single lowercase ASCII SQL identifier unchanged unless it starts with
reserved `dagster__`. Otherwise encode each nonempty component as UTF-8 hex,
join with underscores, and prepend `dagster__`. Hex cannot contain underscores,
so boundaries and case are retained without hashes or process-local registries.
Reject empty paths/components and NUL.

Without schema_name, encode the prefix path as the schema (public when absent),
and encode the final component as the table. Encode an explicit public prefix
to distinguish it from the implicit schema. With schema_name, encode the entire
asset path as the table and retain existing schema normalization. Both reads
and writes use the same mapping.

Affected existing assets need deliberate migration or rematerialization; never
fallback to ambiguous legacy storage. Metadata exposes the actual target. No
server identifier truncation or length limits are introduced by this change.

Acceptance: distinct target pairs for the reported keys and related aliases;
simple names retain their targets; materializing both reported keys preserves
independently readable output with default and explicit schemas.
