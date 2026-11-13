# Contract

See [intent](intent.md), [plan](plan.md) and
[harness semantics](../../../../benchmarks/upstream-allocation/README.md).

- Pin Go 1.27.3, go-redis 9.23.0 and Arrow Go 18.8.0; capture checksums and
  fixture/source hashes. Never edit the shared module cache.
- Run actual parser code from a private licensed source copy with synthetic
  RESP3 arrays. Validate nil/empty/exact binary/string/integer bytes and complete
  reply consumption. Label fixed-slot decoding a limited benchmark prototype.
- Validate every Arrow type/value/null, retained batches, header reuse, early
  release, concurrent independent streams and final allocator balance.
- Distinguish schema-each, schema-known and schema-once stream contracts. Go
  B/op excludes C allocations and shared buffers; timing is not allocation count.
- Keep all samples and comparison limitations. No Redis server is needed; any
  future actual Redis test must use owned verified Software with active 4x QPF.
- No upstream posts/PR publication until parent review. No external road-test
  code is copied or run without verified licensing and strict TLS/RESP3 setup.
