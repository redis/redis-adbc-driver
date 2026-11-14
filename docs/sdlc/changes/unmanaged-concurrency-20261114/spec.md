# Specification

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Use native per-key HMGET for needed source fields after unchanged Search
candidate discovery. A non-NULL selected value, including an empty string or
binary/NUL string, proves a HASH existed at that single atomic native read.
Return those exact selected values. An all-NULL vector does not prove absence:
read the key with original native HGETALL and emit only that complete later
live image, or omit a missing/non-HASH key. Never combine the first vector with
values or existence from the second observation. The existing SQL residual is
rechecked against the returned image. Unmanaged reads promise no statement
snapshot; the later complete observation remains valid live semantics.

Zero selected fields, overlarge requests or layouts with missing-through values
retain HGETALL. Keep at most 256 selected fields, 1000 commands and a conservative
1 MiB request target per pipeline. A single oversized native key retains the
existing native path. Large values now avoid Lua entirely, with no extra Lua
bulk-string copy; they are never rejected or truncated.

Both HMGET and fallback HGETALL commands carry NoRetry markers. Inspect every
command and account for the outer pipeline outcome before any all-NULL or
permission fallback. Accept only direct native WRONGTYPE as omission. Only an
exact direct native HMGET command-permission refusal selects scan-local HGETALL
compatibility. A wrapped/joined error, direct goredis.Nil, malformed/wrong-width
whole HMGET reply or non-string field is terminal: missing HASHes yield a proper
all-NULL array, never whole-command Nil. Unknowns cannot hide behind a known
refusal or cause another live read. Do not cache permissions or values across
scans. Application source keys/ownership, source membership and metadata remain.

The earlier hot-digest budgeting proposal is superseded: native reads eliminate
the Lua/digest/body path, TYPE/HSTRLEN/ACL-preflight work and script-cache repair.
Its short-key fixture already fit 1000 commands, so it was never evidence for
the archived regression's cause. Historical guarded-reader diagnostics retain
their original source and are not relabeled as native candidate observations.
