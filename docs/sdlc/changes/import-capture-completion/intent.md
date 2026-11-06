# Import capture completion intent

Date: 2026-11-06. Status: PR follow-up, not merged.
Requestor: Albert Wong; implementation and rebase: Codex.
PR: [#332](https://github.com/redis/redis-adbc-driver/pull/332).
Base: `617e282ac2a06c57af9865799ec711ab670becc6`.

Require durable proof that the entire private import journal has been captured
before emitting destination rows, then require exact consumption before seal or
publication. Existing source iteration completes in Go; this change adds a
server-enforced checkpoint and immutable completed input.

Public process resume, takeover ownership and row-ID reservation experiments
remain outside this change. External source HASHes and indexes remain untouched.
See the [specification](spec.md), [plan](plan.md) and [validation](validation.md).
