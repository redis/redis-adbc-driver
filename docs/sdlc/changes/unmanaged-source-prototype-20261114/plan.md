# Exact unmanaged source projection: plan

[Intent](intent.md), [contract](spec.md), [prototype/repro](../../../../benchmarks/unmanaged-source-projection/README.md).

1. Inspect pinned Search lookup/loader/native-key sources and prior rejected
   driver experiment. Complete; no safe existing numeric/alias shortcut found.
2. Complete: add an exported raw-source primitive/core and native adapter to the actual
   pinned rlookup crate; retain a reproducible patch and exact source copies.
3. Complete: execute the same core source's adversarial Rust tests, then type-check the
   adapter against the real pinned redismodule-rs dependency in an isolated
   harness. This does not substitute for the full Search workspace/module suite.
4. Complete: the coordinator ran the owned Software RESP3 repro with a frozen baseline driver/CLI. Fully
   validate driver Arrow types/results and independent native HASH controls,
   inspect ordinary aliased LOAD, and validate LOAD* raw values/overfetch.
5. Complete: record exact toolchain/source/dependency hashes, actual pass/failure/omissions,
   context/size/ACL integration limitations, and cleanup before slot release.

No driver runtime change, native SQL speedup claim, ClickBench run, public
upstream post, release or merge is part of this prototype. Human maintainer
review is required before any production adoption; agent review is not approval.

Actual outcomes and remaining engine-integration gaps: [validation](validation.md).

Review: [PR #441](https://github.com/redis/redis-adbc-driver/pull/441).
