# Specification

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Hot EVALSHA_RO pipelines charge the digest and actual arguments, not the Lua
source that they do not transmit. Full-body per-key eligibility remains bounded.
After every outer pipeline outcome is inspected, definite NOSCRIPT results may
be repaired by independently bounded full-body EVAL_RO pipelines. The 1 MiB
request target and 1000-command limit apply to both forms independently.
Uncertain replies never select repair/fallback, and oversized requests retain
native HGETALL. Selected-value bounds and all source/NULL/type semantics remain.

This budgeting issue affects long keys/fields; the short-key archived fixture
already fits 1000 commands per pipeline. Do not claim it explains that regression.
A separate opt-in profile compares actual guarded retrieval with native HGETALL
at one/four clients, validating source bytes/count/sum and recording complete
observations. Endpoint INFO CPU/statistics are not whole-server CPU evidence.
