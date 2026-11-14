# Validation

[Intent](intent.md), [specification](spec.md), [plan](plan.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

## Guarded-reader attribution before replacement

Source `55f3d91f` ran the opt-in retrieval-only profile through the root-owned
verified Software 4× QPF slot. This is Go source retrieval, not SQL, Search,
Arrow or the Python/C boundary. Every selected source byte/count/sum and every
full native row field was independently validated. One warmup plus three
measured alternating rounds, 32 × 10K-row reads/trial on the 100K/four-column +
ordinal fixture, one/four clients, all completed successfully. Logs are private.

The earlier digest/body request-budget change is present in this diagnostic
source, but the fixture's ordinary short keys still fit the 1000-command batch
limit. No claim that budgeting explains the standard concurrency regression.
Endpoint INFO CPU fields stayed zero, while some network-statistic observations
were cached/unchanged: these cannot attribute CPU or exact bytes and are not
zero server work. Global Go allocation deltas and hook command counts retain
their diagnostic scope. Trial wall seconds include independent validation;
operation samples stop before it and are not saturated-capacity evidence.

## Current native candidate

The native proposal supersedes the digest/body changes and removes the Lua
path rather than leaving dead script helpers. Focused unit, live boundary and
actual full-SQL profiles are pending until the coordinated slot is available.
No archived evidence or old source identity has been changed. Human maintainer
review and approval are not inferred.
