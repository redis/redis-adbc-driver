# PostgreSQL 18 JSON: spec

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong. Source: `9bc9d620`. Status: targeted batch implemented and validated.
Issue: [#98](https://github.com/redis/redis-adbc-driver/issues/98); depends on [PR #375](https://github.com/redis/redis-adbc-driver/pull/375).

Add distinct logical JSON and JSONB types stored in authoritative HASH text fields, with write validation, JSON text preservation and JSONB normalization. Publish UTF-8 Arrow JSON extension metadata with logical SQL identity. Implement key existence, deletion/path deletion, JSONB_SET and bounded/cancellable JSON set-returning functions with PostgreSQL 18 values/types/errors. Keep unsupported JSON forms explicit; do not add skips or widen allowlists.

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).


Native JSON preserves raw text, whitespace and duplicates. JSONB uses normalized
text and exact numeric comparison/hash identity; SQL NULL stays distinct from
JSON null. Plain JSON comparison/grouping is rejected where PostgreSQL has no
operator. JSONB remains outside text Search indexing.

Arrow uses UTF8 with canonical `arrow.json` metadata and the SQL JSON/JSONB
identity marker. Native fields can round-trip through ingestion, including
arrays. Generic incoming extensions retain the existing opt-in storage policy.
New persisted kind values cause older drivers to reject unsupported metadata.
Non-UTF8 result layouts reject explicitly. PostgreSQL and Arrow definitions:
[JSON types](https://www.postgresql.org/docs/18/datatype-json.html),
[canonical JSON extension](https://arrow.apache.org/docs/format/CanonicalExtensions.html#json).

This batch supports key existence (`?`, `?|`, `?&`), text/integer deletion,
path deletion, `jsonb_set`, the each/array-elements/object-keys SRFs and exact
JSONB containment. It does not complete the broader #98 wishlist: text-array
`-` deletion, further mutation/record-population functions and jsonpath operators
remain outside this batch. Portable partition parameter serialization does not
yet admit native JSON kinds. HASH storage does not add RedisJSON collections.
