# Adopt go-redis v9.24.0 for transaction cleanup

Date: 2026-11-24. Source: `ea16062c`. Requestor: Albert Wong, issue
[#650](https://github.com/redis/redis-adbc-driver/issues/650). Status:
implemented for review; no human review.

go-redis v9.23.0 could return a pooled socket that still had WATCH or MULTI
armed after a refused cleanup ([redis/go-redis#4315](https://github.com/redis/go-redis/issues/4315)),
and a later borrower inherited it. The connection's shared pool runs the
driver's WATCH … EXEC paths (`store.go`, `tags.go`, `adopt.go`, `rekey.go`,
`view.go`, `sequence.go` and others) and has no driver-level discard. Only the
per-statement execution pool already refused to lend itself after a failed
transaction or cleanup (`transport.go`), which stays as defense in depth.
[redis/go-redis#4316](https://github.com/redis/go-redis/pull/4316), released in
v9.24.0, retires such sockets.

Adopt v9.24.0 and requalify the behaviour changes that reach the driver.
Preserve the documented contracts: planner probes outside statement
diagnostics, URI settings on both clients, RESP3 only, autocommit only. No
driver-level transaction or consistency features. Historical benchmark records
that name v9.23.0 stay unchanged.

See [spec](spec.md) and [plan and evidence](plan.md).
