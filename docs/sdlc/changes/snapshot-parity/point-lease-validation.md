# Point lookups for snapshot lease operations

Date: 2026-11-05. Parent: 31ade98 (production fdbe75f).
Status: reproduced regression, implementation, focused Software checks, full
native validation, full snapshot races and matched comparisons complete;
full ordinary/platform checks and feature/performance parity remain open.
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
The complete control-script race selection including this case passes 20 test
events, two separately covered operator fixture skips, zero failures; root
2.021 s.

All live checks are sequential on owned Redis Software 8.8.0, verified strict
TLS, noeviction and active/effective 4x QPF; three primaries behind a proxy,
macOS ARM64 / Go 1.27.3. They do not prove Cluster API or reader authorization.
At production source 7c30fa6, versioned native ADBC validation passes 565 tests
and three subtests, eight explicit capability skips, 82.45 s; Python 3.14.10 /
PyArrow 26.0.1 / manager 1.13.0. Library SHA-256:
`a7d1bc5d9279f0c2eaff8debfe83742922d1663ba3d5499091e916513f5641d6`.
Fewer catalog reads alone do not establish latency or throughput parity; the
actual comparisons follow.

## Matched native comparisons

Separate read and sustained-write comparisons each use six isolated native
processes: preceding fdbe75f, point leases 7c30fa6, mutable f4d5b78, then reverse
order. The unchanged eight-case read harness uses 20,000 input rows plus
replacement/deletion, 32 unrelated tables, one warmup and five samples per case.
All fetched values are independently checked in timing; the complete implicit
read also checks Arrow schema. Setup is excluded. All
[read cases, samples and hashes](point-lease-read-comparison.json) are retained.

The full 19,999-row implicit-read pooled median is **671.892 → 599.015 ms**,
10.85% lower in this comparison. Mutable is 127.184 ms, leaving a **4.710x**
gap. Current p95 is 697.212 ms versus preceding 879.087 ms. Two processes and
ten samples/source are limited evidence, not statistical or general workload
guarantees; prior control-cache read comparisons remain separately reported.

The sustained case runs 64 alternating UPDATE RETURNING operations across two
20,000-row tables per process. Every returned value and all 40,000 final rows
are independently verified, with zero caller retries. Publication and complete
RETURNING fetch are timed; setup/final validation are excluded. All
[write samples, intervals and hashes](point-lease-write-comparison.json) remain.

| Build | Median ms | p95 ms | Updates/s |
|---|---:|---:|---:|
| Preceding fdbe75f | 33.027 | 69.868 | 25.969 |
| Point leases 7c30fa6 | 33.511 | 67.233 | 25.047 |
| Mutable f4d5b78 | 5.485 | 6.778 | 173.069 |

The write result does **not** establish an improvement: throughput is 3.55%
lower in this comparison and median is nearly unchanged. Mutable throughput is
6.910x higher. Keep this negative result alongside the read result; constant
record access does not imply all-workload parity.

Both process guards detect zero competing local build/test violations, sampling
before each process and every 500 ms; no database probes overlap timing. Software
settings are verified per process. Python 3.13.16 / PyArrow 26.0.0 / manager
1.13.0; Apple M5 Max, 18 logical CPUs / 36 GiB; Docker 18 CPUs /
25,421,033,472 bytes. This synthetic workload is not ClickBench.

Author review checks moved branches preserve token/root/time/state checks and
argument shape, generation bounds, admission pruning, cancellation and NoRetry.
An expired unrelated pin is left for admission/collection rather than modified
by another reader's validation. The exact 128-slot test covers that transition.
No SQL reader grants or raw row access are broadened. This is not independent
maintainer approval; current full Go/platform and reader/Cluster/recovery gates
remain open.

The complete snapshot race selection at 7c30fa6 passes 451 test events,
29 explicit skips, zero failures; root 363.539 s and all six test-bearing
packages pass. It includes the quota test; the two operator cases require the
separately verified role fixture. Counts include subtests. This is not a
controlled timing comparison or full ordinary/Cluster/platform evidence. Hosted
[Test at 638c308](https://github.com/redis/redis-adbc-driver/actions/runs/42132744174)
is the new current-source run. The earlier
[Test at fdbe75f](https://github.com/redis/redis-adbc-driver/actions/runs/42130363002)
passes ordinary Go on both AMD64 and ARM64, then is intentionally canceled
while snapshot regressions run to release the workflow concurrency slot for
the new implementation. Its unfinished snapshot/native steps are not passes.
