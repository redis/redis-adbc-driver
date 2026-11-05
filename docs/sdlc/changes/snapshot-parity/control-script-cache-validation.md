# Standalone snapshot control script cache

Date: 2026-11-05. Parent: 05b60a6 (production 8d59eeb).
Status: focused Software race, EVAL-only compatibility and full native checks
pass; performance and full updated-source Go checks remain required.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[PR #284](https://github.com/redis/redis-adbc-driver/pull/284).

Every standalone control operation previously sent the 21,883-byte Lua body.
The store now sends its 40-byte SHA-1 digest with EVALSHA. A direct native
NOSCRIPT refusal permits one full-body EVAL with identical declared keys and
arguments. A recognized native top-level EVALSHA command-permission refusal
selects the same fallback and remembers EVAL-only operation for that store.
Row/image pipelines, Lua logic, storage protocol 15 and publication outcomes
remain unchanged. No option or migration is added.

Both attempts retain the existing NoRetry command marker. Transport errors,
wrapped/joined errors, textual lookalikes and Lua/key permission errors never
select a second command. Cancellation is checked before either attempt. An
unknown fallback reply is not resent. Existing durable publication resolution
still handles an uncertain publication outcome without replaying SQL/input.

Redis documents script-cache volatility and the native NOSCRIPT response in
[EVALSHA](https://redis.io/docs/latest/commands/evalsha/) and
[Eval scripts](https://redis.io/docs/latest/develop/programmability/eval-intro/).
The cache is an optimization, not a persistence assumption. The live fixture
flushes the cache twice and observes the actual command sequence
EVALSHA, EVAL, EVALSHA, EVALSHA, EVAL. Every successful validation preserves the
pinned generation, exact BIGINT value and unchanged keys/arguments. This is a
cache-loss check, not failover or multi-shard recovery evidence.

Existing failure-injection observers recognize the known digest as the same
logical control operation; they delegate the original wire command unchanged.
New raw-wire tests do not use this normalization. Unit cases check copied
observer arguments cannot change the digest or declared keys.

The expanded verified Software race selection passes 121 test events, one
explicit EVAL-only fixture skip, zero failures; root 61.919 s. It includes lost
publication, independent publication, generation/SQL/ingest/write admission,
socket timeout, bound parameters, compaction queue, sweep, rekey, Substrait,
retained budget/cancellation and control-cache checks. Counts include subtests.
The cold/flush case exercises the actual store wrapper.

The separate `scripts/snapshot_control_script_software.py` harness provisions a
temporary full operator with EVAL but without EVALSHA on the same owned local
Software database. The final store-wrapper lifecycle case passes (0.16 s;
root 2.005 s): acquire, validate, renew and release produce one denied digest
attempt and four successful full-body commands. The harness verifies effective
4x QPF before Go runs and after removing its own principal. This grants no new
permissions to existing SQL readers and does not close reader authorization.
An earlier harness attempt stopped before Go because ACL reconfiguration
temporarily unset effective routing; it now waits for verified QPF readiness.

All Redis checks above use Redis Software 8.8.0, strict verified TLS,
noeviction and active/effective 4x QPF, three primaries behind a proxy;
macOS ARM64 / Go 1.27.3. They do not prove Cluster API behavior.

Separately, the previously running full ordinary race suite at production
8d59eeb completes with 2,034 test passes, 341 explicit skips, zero failures,
all six test-bearing packages passing; root 1,144.857 s. That process compiled
before this control-cache change and is not evidence for the new executor.
No latency improvement is inferred from the smaller script payload.

At fdbe75f, the versioned native driver passes the full ADBC suite: 565 tests
and three subtests, eight explicit capability skips, 82.38 s; Python 3.14.10,
PyArrow 26.0.1 and manager 1.13.0. Library SHA-256:
`39bfe7b82aa8bb6ae85fccd759d4d9cdeb1b6661cd2cb24d5eb73530ac5a45f0`.
The skipped capabilities do not establish compatibility.

The preceding hosted [Test at 05b60a6](https://github.com/redis/redis-adbc-driver/actions/runs/42125238521)
is terminal failure overall. Both AMD64 and ARM64 pass ordinary Go, snapshot
regressions and native ADBC validation, covering production 8d59eeb. Both fail
the independent restricted TLS partition worker with permission_denied and the
required SELECT-grant gate. Later Cluster/proxy TLS steps skip. The earlier
provisioning step's successful exit does not satisfy its final required gate.
This is neither full reader parity nor coverage of the later cached executor.

## Matched native reads

Two separately retained comparisons each run preceding 8d59eeb, cached fdbe75f,
mutable f4d5b78, then reverse order. Each native process uses the unchanged
eight-case harness, 20,000 input rows, replacement/deletion, 32 unrelated tables,
one warmup and five measured samples per case. Complete fetch and independent
value checks are included in timing; the implicit case also checks Arrow schema.
Setup is excluded. All twelve processes pass. A process census before each
process and every 500 ms detects zero competing test/build violations.

Full 19,999-row implicit-read medians, in milliseconds:

| Comparison | Preceding | Cached control | Mutable |
|---|---:|---:|---:|
| Initial | 601.310 | 704.638 | 130.097 |
| Regression investigation repeat | 584.195 | 561.686 | 124.500 |
| Both, twenty samples/source | 595.828 | 602.433 | 126.319 |

The first result triggered the repeat; neither is discarded. Pooled cached
latency is 1.11% higher than preceding and 4.769x mutable. These limited,
variable results do not establish a stable full-read improvement or regression.
All eight cases and samples remain in the
[raw comparisons](control-cache-read-comparisons.json). Smaller script payloads
alone are not performance parity. Python 3.13.16 / PyArrow 26.0.0 / manager
1.13.0; Apple M5 Max, 18 logical CPUs / 36 GiB, Docker 18 CPUs /
25,421,033,472 bytes. This synthetic workload is not ClickBench.

An initial comparison launch stopped before setup/timing because its required
snapshot URI was absent; it yielded no samples and was corrected before the
two complete comparisons above. Fresh full-source Go suites remain required.

## Matched sustained writes

Six additional isolated native processes run 8d59eeb, fdbe75f, mutable f4d5b78,
then reverse order. Each performs 64 alternating UPDATE RETURNING statements
across two 20,000-row tables. Every returned value and all 40,000 final rows
are independently checked, with zero caller retries. Complete publication and
RETURNING fetch are timed; setup/final validation are excluded. The same
process guard detects zero violations. All
[samples, intervals and hashes](control-cache-write-comparison.json) remain.

| Build | Median ms | p95 ms | Updates/s |
|---|---:|---:|---:|
| Preceding 8d59eeb | 32.700 | 67.099 | 25.073 |
| Cached control fdbe75f | 32.439 | 64.719 | 26.279 |
| Mutable f4d5b78 | 5.603 | 7.389 | 172.175 |

Throughput is 4.81% higher in this comparison, with nearly unchanged median
latency. Mutable throughput remains 6.552x higher. Two processes/source do not
establish statistical or cross-workload guarantees. This limited improvement
does not close performance parity; retain the mixed read results above.

Author review checks direct native refusal classification, cancellation between
attempts, unchanged argument suffixes, NoRetry markers, unknown fallback outcomes,
actual EVAL-only command denials and observer/wire separation. No reader grants,
Lua/storage logic, quotas, protocol or SQL/input replay behavior change.
This is not independent maintainer approval. The full snapshot race selection
at fdbe75f completes with 450 test passes, 28 explicit skips and zero failures;
root 371.868 s and all six test-bearing packages pass. It compiled before the
subsequent point-lease regression/optimization and does not cover that change.
The hosted Test run at fdbe75f remains in progress. Full-suite elapsed times are
not controlled performance comparisons; no speedup is inferred from this run.
