# Predeclared experiment contract

Production behavior and the release script remain byte-identical. Extend only
opt-in release test diagnostics. A separate TLS monitor obtains INFO commandstats
before and after a release. Client hook and socket durations exclude these INFO
calls; record their own before/after request durations separately to reveal any
wait shifted into the monitor requests. Keep only aggregate EVAL/EVALSHA call, execution-microsecond and rejection
counts, never keys, values, users, endpoints or script arguments. Require one
executed script per sample, nondecreasing counters and a successful release;
capability failures or counter contamination fail the diagnostic test only after
normal release/job cleanup and complete output validation, without skips or
weakened assertions. Observation errors never become release command failures. Fresh pins and normal bounded cleanup remain mandatory.

A new untouched dense I/O control and separately labeled instrumented dense job
retain their complete source workload and independent 20K+20K result checks.
INFO adds requests and pacing: compare entire distributions and retain every
attempt. Script commandstats time is server-reported command execution, not a
precise CPU measurement; TLS residual includes dispatch/queue/network/TLS and
cannot identify any single cause. No production optimization follows solely
from a residual. A capability blocker is evidence, not permission to substitute
an unverified server or change security settings.

See [intent](intent.md) and [plan](plan.md).

## Final scope

This contract governed the archived experiment, not a newly supported option.
Per-release commandstats attribution failed its gate; preserve both diagnostic
patches and every attempt as evidence. Default source remains `88774f10`.
See [validation](validation.md) for the initial diagnostic interference and its
separately frozen correction; no server CPU or transport cause was established.
