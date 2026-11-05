# Ray plan release and hosted generation pressure

Date: 2026-11-05. Parent source: ce36c86. Status: reproduced locally; hosted ordinary Go confirmation passes on both
architectures. Later platform/authorization steps remain separate gates. See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and [the hosted record](atomic-compaction-hosted-validation.md).

The terminal amd64 job in [Test at 11213c8](https://github.com/redis/redis-adbc-driver/actions/runs/42098297577)
fails concurrent COMMENT admission. Diagnostics distinguish eight live partition
sets from nine writer pins, with eight committed pinned roots and eight live
owned staging workspaces filling the 16-generation bound. All eight partition
origins are untracked by the Go harness. These are valid capabilities, not
abandoned reader streams that the collector may evict.

The preceding Ray datasource suite creates eight plans and leaves their groups
alive for later replay. Producer Close deliberately preserves those capabilities.
The suite never explicitly releases them after its final retry. Its first two
plans share a root, so those eight groups retain seven distinct old roots.

## Reproduction and correction

The exact preceding ce36c86 Ray adapter, copied into a private fixture, passes
all 17 original tests in 13.14 s with the preserved d1d2778 native library. A
read-only server-clock/catalog observation immediately afterward finds **eight
live partition groups on seven roots**. Running the unchanged COMMENT race
against that database fails in 0.15 s. Its diagnostics exactly match the hosted
shape: eight partition groups, nine writer pins, eight pinned committed roots,
eight live staging workspaces and capacity 16. This provides the missing workload
sequence, rather than another speculative reclamation change.

`RedisDatasource` now captures the driver's public partition release ID and
supports explicit `close()` and context management. Close uses a fresh connection
with the original configuration, releases only its own validated capability, and
is idempotent. A failed release leaves the plan open for explicit cleanup retry.
Existing acquired readers keep their independent pins; new attempts after release
fail. Planning/producer Close and `read_query`'s lazy/retry lifetime are unchanged.
Users wanting explicit early release can scope `RedisDatasource` around the
complete `ray.data.read_datasource` computation. No destructor or automatic
release at the first successful task truncates later retries.

Each Ray test now tracks its own successfully constructed plans and closes them
at teardown, after all read/retry assertions. The guide demonstrates this explicit
lifetime and corrects its obsolete claim that retries lacked a shared snapshot.
No capacity increase, early lease expiry, collector permission change, SQL replay
or generation-format change is used to hide the admission refusal.

The new context/release regression fails against the preceding adapter in 1.30 s
because the context-manager protocol is absent. After the change, **20 Ray tests
pass**, no skips, three upstream deprecation warnings, 14.93 s. These include
worker TLS trust/rejection, complete schemas/native values, retries across writes,
TRUNCATE/DROP replay, idempotent release, survival of an already acquired reader,
rejection of a released descriptor and explicit retry after a cleanup connection
failure. A subsequent independent catalog observation requires **zero live
partition groups**. The same COMMENT race then passes in 0.53 s (root 1.089 s).

The updated executable Ray guide also passes with `--require-read-write`,
including explicit datasource scope and independent output write/read-back.

Runs are sequential on owned Redis Software 8.8.0, strict verified TLS,
three primaries behind its proxy, active/effective 4x QPF and noeviction, on
macOS ARM64. Python 3.13.16, Ray 2.59.0, PyArrow 26.0.0 and ADBC manager 1.13.0.
The native library is the preserved d1d2778 build, SHA-256
`6a51f7633a9b770a68ea7ff9270c1c9d0d4f0b7082af47f723b3f4c7854e461a`.
The local Go COMMENT selections include the pending pre-copy compaction-index
worktree change; its capacity/lease logic is unchanged. These checks do not
substitute for a fresh hosted run of the published final source.

Author review checks explicit ownership, validated ID interpolation, preservation
of live/retry semantics, acquired-reader lifetime, failure cleanup and complete
result assertions. This is not maintainer approval or a restricted-reader,
Cluster/recovery or performance-parity claim. Operational logs remain private.


In [fresh Test at 4e0899b](https://github.com/redis/redis-adbc-driver/actions/runs/42103905305),
the amd64 and arm64 Software jobs both complete their ordinary Go step with
success and advance to snapshot regressions. This verifies the previously
failing amd64 workload sequence after cleanup; the arm64 job has no preceding
Ray datasource step. At observation the overall run remains live. Do not count
pending snapshot/native/restricted-reader/platform steps as passed. This run
covers the Ray release source, before the local 50d6b84 compaction change.
