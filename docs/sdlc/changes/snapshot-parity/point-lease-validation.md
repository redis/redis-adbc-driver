# Point lookups for snapshot lease operations

Date: 2026-11-05. Parent: 31ade98 (production fdbe75f).
Status: reproduced regression, implementation and focused Software checks pass;
native/performance/full-source checks remain required.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[control-cache evidence](control-script-cache-validation.md).

Validation, renewal and release previously enumerated the entire bounded catalog
and decode unrelated pins before accessing their own token. They need only the
format, token and (for writer renewal) private workspace records. The change
moves these existing branches before catalog enumeration, preserving
Redis TIME expiry, root/token/state checks, NoRetry markers and durable outcomes.
Acquisition, quota accounting, publication and reclamation retain their existing
enumeration and expired-pin pruning. No format, option, quota or permission
expansion is introduced.

The regression creates a temporary Software operator explicitly denied HGETALL.
Direct HGETALL must fail; actual store validation/renewal/release must succeed
with the existing script and declared keys. Wrong-root operations, expired
readers and expired writers must still refuse. Only fixture-owned private lease
and workspace records are altered to exercise expiry; catalog definitions and
published data remain untouched. This operator is not a SQL reader and does not
close restricted-reader authorization.

Against unchanged fdbe75f, the new point-lease case fails in 0.10 s at its first
validation with the native Lua ACL refusal for HGETALL. The direct denial check
and separate EVAL-only operator case pass. After moving the three unchanged
branches ahead of enumeration, both real Software operator tests pass; root
2.006 s, point-lease case 0.06 s. The harness removes its own principals and
verifies active/effective 4x QPF afterward, including after the failing run's
cleanup. No existing principal's grants are broadened.

The neighboring race selection passes 126 test events, four explicit skips,
zero failures; root 27.389 s. It includes expiry/cancellation, private writer
fencing, independent publication, admission, sweep, retained and live partitions,
producer/worker release, Substrait and rekey Close. The EVAL-only/point-lease
fixture skips are covered separately above; the Cluster and isolated reader-model
fixtures are absent. Reader production parity remains open. Counts include subtests and are not additive
with the full suites.

An additional real protocol case fills all 128 reader slots and confirms the
next acquisition refuses. It expires two fixture pins, validates/renews a live
pin without pruning either unrelated record, then confirms acquisition prunes
them and admits exactly two replacements before refusing again. Cleanup uses
the normal release operation for every owned token, including refused attempts.

All live checks are sequential on owned Redis Software 8.8.0, verified strict
TLS, noeviction and active/effective 4x QPF; three primaries behind a proxy,
macOS ARM64 / Go 1.27.3. They do not prove Cluster API or reader authorization.
Versioned native validation and matched comparisons remain required. Fewer
catalog reads alone do not establish latency or throughput parity.
