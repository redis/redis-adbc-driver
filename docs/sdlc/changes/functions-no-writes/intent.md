# Read-only Redis Functions under memory pressure

2026-11-25 · Requestor: user, through the RediSearch#12531 session · Source: 9ea7ba76 · Status: implemented; validation in [plan](plan.md)

Every driver operation runs as a Redis Function (no EVAL fallback since
94e123d6). The library registered each body without flags. Redis then treats
every `FCALL` as a write and denies it with `OOM command not allowed` while the
server is over `maxmemory`. Pure reads fail with it, including the probe each
new connection runs. Transient memory pressure therefore becomes read and
connection failures. The investigation of
[RediSearch/RediSearch#12531](https://github.com/RediSearch/RediSearch/issues/12531)
raised it: there, the next command after a Flex quota overflow was a read-only
directory Function. On Redis 8.10.2 with `maxmemory`, an unflagged read-only
body returned the OOM reply; the same body with `flags={'no-writes'}` succeeded
under `FCALL_RO`.

Flex, as checked by the requesting session: on a dedicated Flex database
(Software 8.2.6-58, Redis 8.8.0, verified 4x QPF, 2.56 GB `memory_size`, 256 MiB
RAM part) at its logical limit, the server refused `HSET`, `SET` and unflagged
`FCALL` with the core `maxmemory` message. The same body flagged `no-writes`
answered under `FCALL_RO`, over 15 s and four repetitions. That result is
claimed. The RAM-part (`bigredis-max-ram`) refusal never fired, even with RAM
at 9.4x its limit, so nothing is claimed about it.

Desired behavior: Functions that cannot write are registered `no-writes` and
invoked with `FCALL_RO`. They keep answering over maxmemory, are refused any
write by Redis, and need only read key permissions. Writes still fail under OOM.

Affected consumers: every driver connection, since the probe moves to
`FCALL_RO`. Restricted SQL RBAC readers and retained-partition workers are
affected as well. Their ACLs gain `+fcall_ro`; the read-only retained worker
replaces `+fcall` with it.

Exclusions: RESP3-only and Functions-required behavior are unchanged. No EVAL
fallback is added, and no write body is flagged. `FUNCTION LOAD` remains
denied under OOM, so a new driver version still installs only below the limit.
Reads from replicas stay unsupported (`read_only` and related options are
rejected), even though `FCALL_RO` would let Redis route them there. There is no
automatic migration of existing dedicated SQL RBAC ACLs; that matches the
documented "not migrated or upgraded" contract.

See [specification](spec.md) and [plan and validation](plan.md); implemented in [redis/redis-adbc-driver#683](https://github.com/redis/redis-adbc-driver/pull/683).
