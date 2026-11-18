# Intent: retain live statement pins after an uncertain heartbeat

Date: 2026-11-18. Source: `06302c39`. Status: implemented and locally verified; full-scale validation outstanding.
Requestor: Albert Wong. Author: Codex. Issue: [#490](https://github.com/redis/redis-adbc-driver/issues/490).
Related: [specification](spec.md), [plan and evidence](plan.md).

## Problem

The issue reports unmanaged COUNT(*) failing after 79–87 seconds on an immutable
99,997,497-row Redis Software fixture. The heartbeat currently treats any failed
10-second renewal as permanent ownership loss and releases its two-minute server
pin. A transport error cannot establish whether renewal ran or the pin expired.
The reported full-scale cause remains unconfirmed without that fixture's trace.

## Desired outcome

Allow a still-live pin to survive an uncertain renewal while retaining mandatory
server proofs, expiry fencing, cancellation and bounded cleanup. Confirm behavior
with deterministic lost-reply tests and a fully consumed unmanaged SQL count.

## Constraints and exclusions

Do not change SQL semantics, storage format, server lease duration, renewal cadence,
query planning, or replay SQL/input mutations. Do not claim full-scale ClickBench
validation or performance from small fault-injection fixtures. No new deployment,
100-million-row reload, release, or historical benchmark relabeling is in scope.

## Success and open decisions

Unknown renewal outcomes retain server validation; the next scheduled renewal
requires a positive live-token/root proof. Expired or refused pins fail closed.
Maintainer approval and full-scale reproduction remain outstanding.
