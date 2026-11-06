# Inspecting streamed results with usage accounting enabled

Date: 2026-11-05. Parent: 797ab96; production remains a48ccc9.
Status: reproduced default-enabled wrapper failure and focused/neighboring
enabled-accounting Software checks pass. See [intent](intent.md),
[spec](spec.md), [plan](plan.md) and
[upstream reconciliation](current-main-20261105-validation.md).

The upstream usage decorator preserves the Arrow reader API but changes the
concrete type returned by ExecuteQuery. Snapshot expiry, Substrait, projection,
prefetch, union and wide-scan tests inspect internal stream state through direct
type assertions. With enabled accounting, the actual result is a usageReader;
the old inspection fails before it can exercise the intended lifecycle.

The coordinated production-reader work first observes this in its enabled
snapshot run. Independently, the existing expired-reader/cancellation case
reproduces `not streaming: *redis.usageReader` in 0.13 s on the owned Software
fixture. An earlier setup attempt supplies an invalid traffic-class value and
fails before that repro; it is corrected to the supported ci class and is not
counted as a product failure.

A shared test-only inspectionReader unwraps only the metrics decorator for
concrete state inspection. Tests still call Next/RecordBatch/Retain/Release on
the original public result, retaining accounting and Arrow ownership. The raw
internal execResult cast is unchanged: it precedes statement decoration.
Production reader attachment, lease validation and wrappers are not modified.

Common SQL/retained and disposable snapshot fixtures collect metrics locally
when enabled, with an empty endpoint and synthetic installation identity. They
do not publish synthetic workload data or create persistent identity files.
Explicit telemetry options, including an explicit identity-file fixture, take
precedence. Accounting is not forced off to hide the wrapper. Standard CI's
disabled-telemetry selection remains separate evidence.

With telemetry enabled and local-only, the expiry/cancellation, Substrait,
materialized-view renewal/Open-recovery and actual usage-accounting selection
passes 15 test events, zero skips/failures, root 9.343 s. The usage case checks
complete returned row accounting, early release and cancellation. These run
through the original public wrappers. The additional public projection,
early-release, wide-fetch, prefetch, DISTINCT and UNION ALL selection passes
36 test events, one explicit restored-profile skip, zero failures; root 49.189 s.
All these checks keep telemetry enabled with local-only collection. The absent
restored benchmark fixture is not counted as a pass; no ClickBench is run.

The final telemetry/control unit race selection, with live Redis endpoints
unset, passes 37 test events, six expected integration/profile skips, zero
failures; root 1.987 s. Those skips do not add server compatibility evidence.

Live checks use sequential owned Redis Software 8.8.0, strict verified TLS,
noeviction and active/effective 4x QPF, three primaries behind its proxy;
macOS ARM64 / Go 1.27.3. They do not prove Cluster API or production reader
authorization. The separate protocol16 work is not integrated at this point.
No performance gain is claimed from test inspection changes.
