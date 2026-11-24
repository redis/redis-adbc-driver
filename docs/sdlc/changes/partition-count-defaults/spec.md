# Specification

The existing adbc.redis.partition_count accepts string and int64 values 1..1024
at database, connection and statement scope. Zero is only an internal unset value
resolving to 8, never a public unlimited/reset value. Existing statement validation
and incremental-discovery reset behavior remain. Invalid values fail atomically.

Open copies the database scalar into the new connection; NewStatement copies
the connection scalar into the new statement. Explicit statement setters override
that copy. Later parent changes never mutate existing children, invalidate active
streams/descriptors or reset existing incremental discovery. This follows existing
scalar option initialization/concurrency patterns, not live shared configuration.
Typed methods delegate every unrelated key to the same prior base/typed handlers,
including connection shared-read-snapshot retention. No Redis data operation,
query eligibility, partition format, count cap, worker scheduler or SQL changes.

Partition_count requests a maximum split count, not CPU concurrency. A generic
scanner can forward a database value 16 and independently schedule up to 16 reader
connections; actual returned count can be lower. Incremental=true is unnecessary
for obtaining all current deferred descriptors. Live 16-read compatibility/performance
requires separate verified Software 4× QPF testing; unit options tests cannot
establish it.
