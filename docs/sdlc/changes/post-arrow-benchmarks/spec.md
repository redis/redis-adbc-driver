# Contract

Use exact source 426fdb88 with a Makefile-versioned native library and GetInfo
verification. Dedicated Software RAM, active verified 4x QPF/six Search workers,
mnp/32 connections, one shard, TLS/RESP3/noeviction; no endpoint substitution.

Reuse typed 100K x 105 reads: one/four readers, one/eight tasks, 512/4096 batch
ceilings, one warmup plus six measured repetitions. SQL and redis-py paths share
the same data contracts/timing/validation as the historical comparison.

Matched writes use 10K x 105 public fields plus ordinal; SQL INSERT, Arrow append
and pipelined HSET batch up to 1000 rows; one/four writers, warmup plus three
measured repetitions. Table/index preparation is outside timing. Managed and
unmanaged Arrow differ in publication/ownership; __key is explicit for unmanaged.
Keep partial/conflicted attempts failed, with no successful throughput claim or
retry. Validate all complete outputs and visible partial values independently.

Separate creation suite includes schema/index work in timing: managed/unmanaged
Arrow create (one writer), create_append (one/four), and native membership-index
creation plus HSET (one/four). Bench namespace setup is excluded. Managed indexes
and native/unmanaged membership indexes differ; this is workload cost, not an
isolated index-creation or durability comparison. No replacement case.

Record complete coverage and measured distributions, source/binary hashes,
actual environment and limits. Operational identifiers, credentials, data, raw
logs and licenses remain private. See [intent](intent.md), [plan](plan.md).
