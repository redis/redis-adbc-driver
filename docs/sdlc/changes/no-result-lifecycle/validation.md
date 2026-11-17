# Validation

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

The [new result and limits](../../../../benchmarks/crud-execution/results/no-result-lifecycle-2026-11-17/README.md)
record six paired permission, four cancellation and four successful-mutation /
Close groups passing on native macOS arm64 against owned Redis Software Redis
8.8.0 with verified effective 4x QPF, six workers, mnp/32 and noeviction.

Actual driver source is clean `beec721216e6c4c3ca448fe162534209b7a7f34e` in Go
build metadata; all shipping code is unchanged by this follow-up. The script's
own hash and library hash are in the result. Actual Software principals deny
source mutations while permitting metadata bookkeeping. Direct raw probes
establish those refusals independently. Both public paths retain the existing
IO/empty-SQLSTATE error classification; no new permission mapping is claimed.

Cancellation is synchronized with foreign parameter production, not a sleep or
large query. The producer must return cooperatively before execution can join.
Each path retains CANCELLED/57014, exact data, one input consumption and reuse.
Explicit Close happens after join; retained Arrow values remain valid afterward.

`python3 -m py_compile scripts/no_result_lifecycle.py` passes. The existing four
no-result caller/auditor unit tests, four CI parallel-suite helper tests and nine
coverage-matrix tests pass.
CI now executes this screen on its owned Software fixtures on both architectures
and uploads only sanitized JSON. Its actual outcomes will be linked from the PR;
local results do not establish that CI passed.

Normal exceptions run scoped object/principal cleanup. A supervising process
bounds a wedged native call; if it kills the child, the fixture owner must tear
down the entire disposable fixture (CI already does so in its always-run step).
Private logs retain failed fixture/harness attempts. Frozen timing results are
unchanged. No performance, complete lifetime or GC-policy acceptance is claimed;
#447 remains open for stronger full-path lifecycle/performance qualification.

Owned container/network and generated fixture credentials were removed after
validation. The other running container IDs were compared before/after and
preserved. Resource inventories and raw management/driver logs remain private.

Self-review covered the typed/raw oracles, public caller selection, foreign
producer ownership, cancellation/join/Close ordering, scoped principals/source
cleanup, sanitized outputs and evidence limits. No shipping implementation is
changed. Human maintainer review is pending.

The final harness additionally passes the same complete screen on a fresh
three-shard Software fixture matching CI's topology, still with native macOS
clients. It rechecks effective QPF after each principal change before the raw ACL
probe. The one-shard artifact remains unchanged and identifies its earlier
harness; the separate three-shard artifact hashes the final harness. Both owned
fixtures and generated credentials were removed, with other running containers
preserved. [PR #465](https://github.com/redis/redis-adbc-driver/pull/465) merged the base
coverage while the final three-shard run was completing. The stricter per-probe
gate and three-shard artifact are a separate follow-up. CI must qualify the
newer main dependency set and Linux architectures separately.
