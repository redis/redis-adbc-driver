# Contract and limits

- Run actual pinned go-redis v9.23.0 parser primitives in a private source copy;
  keep its license/provenance and never modify the shared module cache.
- Compare actual SliceCmd decoding with fresh owned string/null result vectors
  for every HMGET-shaped array. Owned result slots and string payloads survive
  later replies, parser/input reuse and changes to another result.
- Distinguish NULL from empty strings, retain field order/exact bytes, and support
  variable-length values beyond the former fixed byte-slot prototype's limit.
  Incomplete/server-error/NULL-array replies return no partial owned result.
- Measure all new row-vector allocations inside timing. Only the common outer
  page, wire fixture and parser are prepared outside timing. Complete values and
  null masks are validated before/after each case; every field is consumed.
- Cover 256/1024/4096 rows ×105 fields with 32-KiB/1-MiB readers. Keep every
  sample and separate parser-only effects from network/TLS/hooks/command
  construction/Arrow costs or end-to-end throughput/RSS claims.
- This is a prototype for a future typed-result contract, not an implementation
  of a supported pipeline/async command API. Result-inspecting hook integration,
  command lifecycle/cancellation/routing/retry compatibility and public naming
  still need upstream design. No borrowed values or unsafe payload reuse.

See [intent](intent.md) and [plan](plan.md).
