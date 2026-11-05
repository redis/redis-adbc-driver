# Standalone snapshot control script cache

Date: 2026-11-05. Parent: 05b60a6 (production 8d59eeb).
Status: focused Software race and EVAL-only compatibility checks pass;
native performance and full updated-source checks remain required.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[PR #284](https://github.com/redis/redis-adbc-driver/pull/284).

Every standalone control operation previously sent the 21,883-byte Lua body.
The store now sends its 40-byte SHA-1 digest with EVALSHA. A direct native
NOSCRIPT refusal permits one full-body EVAL with identical declared keys and
arguments. A recognized native top-level EVALSHA command-permission refusal
selects the same fallback and remembers EVAL-only operation for that store.
Row/image pipelines, Lua logic, storage protocol 15 and publication outcomes
remain unchanged. No option or migration is added.

Both attempts retain the existing NoRetry command marker. Transport errors,
wrapped/joined errors, textual lookalikes and Lua/key permission errors never
select a second command. Cancellation is checked before either attempt. An
unknown fallback reply is not resent. Existing durable publication resolution
still handles an uncertain publication outcome without replaying SQL/input.

Redis documents script-cache volatility and the native NOSCRIPT response in
[EVALSHA](https://redis.io/docs/latest/commands/evalsha/) and
[Eval scripts](https://redis.io/docs/latest/develop/programmability/eval-intro/).
The cache is an optimization, not a persistence assumption. The live fixture
flushes the cache twice and observes the actual command sequence
EVALSHA, EVAL, EVALSHA, EVALSHA, EVAL. Every successful validation preserves the
pinned generation, exact BIGINT value and unchanged keys/arguments. This is a
cache-loss check, not failover or multi-shard recovery evidence.

Existing failure-injection observers recognize the known digest as the same
logical control operation; they delegate the original wire command unchanged.
New raw-wire tests do not use this normalization. Unit cases check copied
observer arguments cannot change the digest or declared keys.

The expanded verified Software race selection passes 121 test events, one
explicit EVAL-only fixture skip, zero failures; root 61.919 s. It includes lost
publication, independent publication, generation/SQL/ingest/write admission,
socket timeout, bound parameters, compaction queue, sweep, rekey, Substrait,
retained budget/cancellation and control-cache checks. Counts include subtests.
The cold/flush case exercises the actual store wrapper.

The separate `scripts/snapshot_control_script_software.py` harness provisions a
temporary full operator with EVAL but without EVALSHA on the same owned local
Software database. The final store-wrapper lifecycle case passes (0.16 s;
root 2.005 s): acquire, validate, renew and release produce one denied digest
attempt and four successful full-body commands. The harness verifies effective
4x QPF before Go runs and after removing its own principal. This grants no new
permissions to existing SQL readers and does not close reader authorization.
An earlier harness attempt stopped before Go because ACL reconfiguration
temporarily unset effective routing; it now waits for verified QPF readiness.

All Redis checks above use Redis Software 8.8.0, strict verified TLS,
noeviction and active/effective 4x QPF, three primaries behind a proxy;
macOS ARM64 / Go 1.27.3. They do not prove Cluster API behavior.

Separately, the previously running full ordinary race suite at production
8d59eeb completes with 2,034 test passes, 341 explicit skips, zero failures,
all six test-bearing packages passing; root 1,144.857 s. That process compiled
before this control-cache change and is not evidence for the new executor.
No latency improvement is inferred from the smaller script payload.
