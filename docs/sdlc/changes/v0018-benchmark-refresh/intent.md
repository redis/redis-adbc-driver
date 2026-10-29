# Intent: v0.0.18 benchmark evidence

Date: 2026-10-29. Requestor: Albert Wong. Status: proposed PR; not merged or
human-approved. Base: `46d438735ddb44e3509ad92ce4ed06ed9528c642`.

## Problem

Presentation benchmark claims needed measurements from the v0.0.18 driver and
an inspectable repository record. The bulk-loading runner had remained local.

## Desired outcome

Publish the completed seven-suite local refresh, reproducible trial arithmetic,
and the unchanged bulk-loading runner with its existing tests. Preserve misses
and limitations, including the earlier first batch from redis-py.

## Constraints and exclusions

Redis Software and verified 4× QPF are the ordinary test baseline. The two
explicit QPF experiments vary factors and restore 4×. The user deferred the
100-million-row reruns. No production code or historical benchmark rewrite.
Operational artifacts, infrastructure identifiers, credentials and licenses stay
private; the Google Slides deck remains an external artifact.

## Success and evidence

See [spec](spec.md), [plan and checks](plan.md), and the
[benchmark report](../../../../benchmarks/v0018-refresh/2026-10-29/report.md).
There is no claim of a pure version effect across differing server environments.
