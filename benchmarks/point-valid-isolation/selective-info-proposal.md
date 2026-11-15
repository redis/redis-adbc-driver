# Proposed selective FT.INFO output for bounded readiness proofs

Status: proposal only; no server API, implementation or measured benefit is
claimed present. No upstream issue/comment has been submitted.

The [official command reference](https://redis.io/docs/latest/commands/ft.info/)
documents `FT.INFO index` as a read-only Search command. The local driver's
[native segment validation](../../go/snapshot_validation_batch.go) consumes only
the exact published prefix, document count, indexing state, completion fraction
and indexing-failure count. It currently receives the full reply, including
schema/attribute descriptions and unrelated memory/cursor/statistics fields.

The diagnostic's 24-public-column schema produced approximately 8 KiB for even
one native index-proof reply. This is an observation for that schema/version,
not an API bound. A fixed two-index grouping improved the narrow diagnostic but
amplified the wide eight-pending-probe diagnostic to about four times baseline;
that grouping change was rejected.

A useful upstream capability would select named native fields while preserving
the same index identity, command/index permissions, routing, warning/error and
read-only semantics. It must not expose an unversioned success bit or reuse
stale readiness. Every statement still needs fresh native authority and its
existing pin/descriptor ownership checks. Exact prefix/cardinality/readiness
fields, per-request failures, missing indexes and unknown replies must remain
distinguishable.

Possible field-selection syntax should be chosen by upstream; this proposal
does not claim a supported `FIELDS`, `SELECT` or other command option. A compact
response would reduce per-query transfer and decoding independent of the
unresolved generic large-TLS-response wait. Any latency improvement would still
need normal full-fetch Python/ADBC measurements and wide/deep negative controls.

This request is separate from exact unmanaged source-HASH projection/reductions
in [Search #12334](https://github.com/RediSearch/RediSearch/issues/12334). It must
not replace authoritative row reads or weaken exact signed-int64/NULL semantics.
