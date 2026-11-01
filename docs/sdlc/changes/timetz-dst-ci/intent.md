# Intent: keep TIMETZ CI assertions correct across seasons

Date: 2026-11-01. Source: `7d6fb15`. Requestor: Albert Wong.
Status: implemented; maintainer review pending.
Inputs: [failing Test run](https://github.com/redis/redis-adbc-driver/actions/runs/41329865605).
See [specification](spec.md) and [plan and evidence](plan.md).

## Problem

All eight Redis/architecture jobs fail `TestTimeTZSessionZoneColumns` after
New York returns to standard time. Its January timestamp fixture becomes
`10:00-05`, which also matches today's offset-free `10:00`. Both the direct
filter and the stored view correctly return rows 8 and 10; the test expects 8.

## Desired outcome

Assert the correct rows in both seasons and retain coverage of the view's
creation-zone literals. Driver behavior and workflow coverage stay unchanged.

## Success and evidence

Reproduce the original failure on verified Redis Software with 4x QPF, pass
the corrected integration test, and exercise January and July comparisons
with fixed statement times. No unresolved implementation decisions.
