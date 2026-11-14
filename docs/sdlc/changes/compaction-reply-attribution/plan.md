# Plan

1. Freeze this pre-execution contract and add a strict commandstats parser with
   endpoint-free malformed/missing/reset/delta/privacy tests.
2. Add an opt-in monitor around the existing actual release call. Keep existing
   profile timers and I/O counters outside monitor requests. Preserve exact source
   command arguments and the original no-retry/release hooks.
3. Under the root-owned serialized heavy slot, run focused parser race tests,
   then capability preflight and one unchanged dense I/O control plus one dense
   commandstats attribution job. Use only Redis Software through make redis-up,
   effective active 4x QPF, verified data TLS/RESP3 and the recorded resource limits.
4. Record every attempt, source/toolchain/settings and all observed distributions;
   independently audit arithmetic, source scope and privacy. Return the database
   empty/index-free and all clients closed; root owns final infrastructure cleanup.
5. Request parent source/evidence review before push or PR. No automatic merge.

See [intent](intent.md) and [spec](spec.md). Execution has ended; see [validation](validation.md).

## Retained first attempt and diagnostic correction

Source `549cbd4a` passed the Go profile but its sample audit rejected all 634
commandstats observations. Returning observer failures as release errors caused
one existing bounded idempotent cleanup retry per release; this was measurement
interference, not a production defect or valid server attribution. Preserve that
entire attempt and the 317-release unchanged control. The corrected diagnostic
records before/after fixed aggregate counters and a fixed reason, preserves the
original release outcome, then fails its sample-validity assertion after the full
job, independent row checks and ordinary cleanup. Freeze before any new run.

## Final decision

Both focused race runs completed. The unchanged dense control validated all rows.
The first instrumented attempt was rejected after sample audit despite Go exit0;
the corrected attempt completed normal releases/output validation and then failed
its strict attribution assertion. No further rerun or production optimization.
Archive both exact diagnostic/test patches on an evidence-only branch from the
baseline. Parent final source/evidence review found no blockers and authorized
PR publication after completed shared infrastructure cleanup. Cleanup is complete;
see [validation](validation.md). No merge or human approval is claimed.

Review: [PR #430](https://github.com/redis/redis-adbc-driver/pull/430) is open; no merge or human approval is claimed.
