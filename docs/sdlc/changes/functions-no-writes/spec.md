# Read-only Redis Functions specification

2026-11-25 · Source: 9ea7ba76 · Status: implemented

## Contract

- A body is declared read-only with `newReadOnlyRedisFunction(body)` only when
  no path through it can issue a write or may-replicate command.
  `redis.acl_check_cmd` only inspects permissions and doesn't count as a write.
  `go generate .` records those declarations in `redisReadOnlyFunctionBodies`.
  The generator refuses two cases: a body declared both read-only and writable,
  and a read-only body invoked through a raw `redisFunctionName` call, which
  hard-codes `FCALL`.
- The library registers read-only bodies as
  `redis.register_function{function_name=…, callback=…, flags={'no-writes'}}`
  and every other body in the unchanged positional form, without flags.
- The generated manifest alone selects the command. `redisFunction.Command`,
  `Args`, `Run`, the retained `call` helper and `snapshotControlExecutor.runScript`
  send `FCALL_RO` for a read-only name and `FCALL` otherwise. A stale declaration
  can't send `FCALL_RO` to an unflagged function, which Redis would refuse.
- The Functions version frames each body with its flags. Any flag change renames
  every entry point and the library, so a library from an older driver and this
  one load side by side; neither replaces the other.
- The snapshot key-scoping hook treats `FCALL_RO` keys like `FCALL` keys. Write
  fencing is unchanged; `FCALL_RO` is never fenced.

## Audited read-only entry points

| Entry point | Commands it can issue |
|---|---|
| Connection probe (`return 1`) | none |
| Unmanaged replacement permission preflight | `acl_check_cmd` only |
| Unmanaged replacement inspection | `TYPE`, `DUMP`, `PEXPIRETIME`, `acl_check_cmd` |
| Snapshot reader-domain discovery | `acl_check_cmd` only |
| Snapshot catalog write capability | `acl_check_cmd` only |
| Reader namespace read | `SCARD`, `MEMORY USAGE`, `SSCAN` |
| Snapshot operation status | `HGET`, `TIME`, `EXISTS`, `TYPE` |
| Snapshot point read | `HGETALL`, `HMGET`, `ZCARD`, `ZSCORE` (through `checked`) |
| Snapshot Search admission gate | `HGET`, `TIME`, `ZCARD` |

The other 39 bodies stay unflagged because each writes on some path. That
includes the directory index notification (`HSET` of the row-ID field), reader
lease control (`HSET`/`HDEL`/`SADD`), snapshot control, mutation fences, GC,
compaction, metadata COW, retained-result scripts, live-registry CAS, Flex
row-ID allocation and the unmanaged row scripts. Effects replication (the only
mode since Redis 7) makes `TIME` in these bodies irrelevant to the flags;
`no-writes` bodies replicate nothing.

## ACL consequences

`FCALL_RO` is a separate command with read-only key specifications. Every
driver user needs `+fcall_ro` for the probe. Writers need `+fcall` and
`+fcall_ro`. The generated reader ACL grants `+fcall_ro` inside the same
`(+fcall +fcall_ro ~catalog ~snapshot*)` selector, so its declared-key
envelope is unchanged. A read-only retained worker needs `+fcall_ro` and no
`+fcall`. Without the grant, a connection fails with `Redis Functions are
required: NOPERM … 'fcall_ro'`. Dedicated SQL RBAC ACLs provisioned from an
older `reader-acl` must be re-provisioned (see [SQL RBAC](../../../sql-rbac.md)).

## Measurable checks

- Unit: the manifest's read-only set equals the audited set; every body has the
  registration form and command matching its flag. Read-only bodies issue only
  literal read commands, and every unflagged body issues a write command. Each
  flag changes the version, and no name carries the unflagged version.
- Integration: over maxmemory on an owned database, unflagged `FCALL` is denied
  with OOM. The probe, the namespace read and every flagged body run under
  `FCALL_RO`, and none creates a key. `FUNCTION LIST` shows `no-writes` on
  exactly the read-only entry points. `FCALL_RO` of an unflagged entry point is
  refused.

See [intent](intent.md) and [plan and validation](plan.md).
