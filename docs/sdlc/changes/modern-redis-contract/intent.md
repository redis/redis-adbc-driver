# Modern Redis contract

2026-11-22 · Requestor: user in this chat · Source: 1d0d0b3b · Status: implemented and locally validated

The user requested a driver without legacy compatibility paths and explicitly
confirmed that named ACL credentials and Redis Functions should be required,
including breaking password-only and EVAL/EVALSHA compatibility.

Require explicit ACL usernames, RESP3 and Functions; use incremental set
iteration. Preserve SQL semantics, HASH ownership, atomic publication, durable
outcome resolution and the rule against replaying uncertain writes. No queues or
Pub/Sub compatibility API exists to migrate. HASH and STRING primitives,
immutable serialized metadata and SQL JSON values are deliberate storage
representations, not a fallback from Redis JSON. Native JSON storage is outside
this change. Historical measurements retain their original provenance.

See [specification](spec.md) and [plan and validation](plan.md).
The driver PR targets the modern-only contract above. Related skill guidance is tracked in
[redis/agent-skills#61](https://github.com/redis/agent-skills/issues/61).
