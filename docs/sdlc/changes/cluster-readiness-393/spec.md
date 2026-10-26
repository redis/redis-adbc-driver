# Contract

Date: 2026-10-21. Status: implemented; human approval pending.
See [intent](intent.md), [plan](plan.md), and issue #393.

Only errors implementing the Redis server error interface and matching one of the
two exact known messages qualify as schema convergence. Wrapped server errors are
recognized. Similar transport errors, unknown replies, missing indexes and denied
permissions retain their existing terminal behavior.

The shared readiness gate returns ADBC InvalidState for convergence. The existing
ALTER readiness loop polls at 10 ms with context cancellation/deadlines; it does
not replay FT.ALTER or publish new metadata early. Partition readers reject the
uncertain range immediately. Existing attribute discovery retains its maximum
20 attempts at 50 ms and now recognizes both server forms.

An indefinitely divergent schema can continue waiting, like indefinitely active
indexing; callers should provide a context deadline. No new public option,
credential, ACL requirement, trust-store change, or data layout is introduced.

Upstream's [FT.INFO reducer](https://github.com/RediSearch/RediSearch/blob/6c6dfd6301ba547e01355d216ef3e9d0b6bd2436/src/coord/info_command.c)
reports this error when aggregated shard field counts differ. This explains the
observed CI replies; it is not a guarantee that every inconsistent deployment
will recover.
