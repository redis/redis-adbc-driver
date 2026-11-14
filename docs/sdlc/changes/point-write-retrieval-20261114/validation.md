# Verification and review — November 14, 2026

[Intent](intent.md), [spec](spec.md), [plan](plan.md). Requested by Albert Wong;
implemented and independently cross-reviewed by Codex subagents. Human approval
and merge are not inferred. The complete [native report](../../../../benchmarks/oltp-query-efficiency/2026-11-14/report.md)
is the authoritative final comparison, including regressions and limitations.

## Actual checkpoints

Frozen native candidate production source is
`c63477a9cb2509b3b3c484f0c6401e31b692ded9`; baseline is merged
`0b21647831d13b0c10e652b7095515b0f5a74069`. Both driver/CLI builds and identical
harness copies have recorded pre/post hashes. No production changes followed
measurement. Later changes publish evidence and documentation only.

| Check | Actual source | PASS | SKIP | FAIL |
|---|---|---:|---:|---:|
| Initial integrated race selection | `665c3f1b` | 80 | 2 | 0 |
| Ordinary Software all Go packages | `665c3f1b` | 1,164 | 374 | 0 |
| Full separate snapshot suite | `00553f22` | 306 | 62 | 0 |
| Pre-correction native boundary race | `00553f22` | 88 | 2 | 1 |
| Corrected final focused race | `c63477a9` | 91 | 2 | 0 |

Commands, full checkpoint commit IDs and every skipped test name are retained in
[checks.json](../../../../benchmarks/oltp-query-efficiency/2026-11-14/checks.json).
Ordinary tests intentionally unset the snapshot URI; snapshots ran separately.
Every live command used the same owned Redis Software fixture created through
`make -C go redis-up` and independently gated for active/effective 4× QPF,
six workers/mnp32, noeviction and certificate-verified TLS/RESP3. Packages ran
sequentially with caching disabled. Final race selection explicitly includes
acquisition/descriptor proof tests and every unmanaged guard regression.
Snapshot source predates only the unmanaged exact refusal-token recognition
correction, covered in the final race run. Optional operator fixtures/topologies,
backups/profiles and capability skips do not establish compatibility.

The failed boundary check is retained as
[pre-fix-regression.json](../../../../benchmarks/oltp-query-efficiency/2026-11-14/pre-fix-regression.json).
Redis prefixes custom one-token script errors with `ERR`; exact bare-token-only
recognition failed on the live oversized-value case. The corrected implementation
accepts only four exact direct native spellings, bare or with one `ERR ` prefix;
wrapped/joined/altered outcomes fail closed. The same native boundary passes in
the final race run, without relaxing its assertion.

Eleven offline Python comparison/audit tests pass, as do the archived November 13
audit, managed diagnostic audit (96+96 observations), final full-pair audit,
formatting and `git diff --check`. The final native pair retains **168 validated
records per build**, all **42 comparisons** and 5,472 measured observations:
zero failed native trials, conflicts, retries or discarded samples. Builds
finished before both matrices; baseline and candidate ran consecutively with no
other heavy work. This is not randomized/interleaved evidence.

## Results and limitations

Single-client managed UPDATE/DELETE improve 1.48×/1.41×; known-key reads in both
modes improve about 13–14%. Unmanaged filtered projection improves 3.31×/1.81×
with one/four clients; filtered COUNT/SUM improves 2.39× with one client.
**Four-client unmanaged filtered COUNT/SUM is 38.1% slower, managed INSERT 15.9%
slower, and managed filtered COUNT/SUM 3.7% slower.** Managed four-client DELETE
and unmanaged single-client grouped aggregation are essentially flat. The full
report retains exact p50/p95/throughput and redis-py multiples for every case.
Three measured trials do not establish causal attribution or confidence bounds.
The driver remains much slower than direct redis-py commands for OLTP.

The earlier [managed profile](../../../../benchmarks/managed-point-images/2026-11-14/report.md)
and [point proof](../point-read-proof-pipelines/validation.md) /
[unmanaged retrieval](../unmanaged-live-projection/validation.md) diagnostics keep
their own sources and scope, including earlier slower observations; their timings
are not relabeled as the final guarded native build. No new bulk/transfer,
ClickBench, Flex, Flight SQL or native Cluster API result is claimed.

## Review and ownership

Independent cross-review inspected unchanged native proof keys/arguments and
error precedence; shared pins; no permission/lease/value caching; managed
newest-winner/tombstone/string/int64/NULL/NUL and MaxInt64 row-ID behavior;
restricted/strict/multi-key compatibility; external source ownership/type/NULL
policy; request/selected-value bounds; cursor/Arrow/cancellation lifetime.

Cross-review reproduced composed absence/refusal failures masking lost replies;
those now require direct native proof and full pipeline inspection before any
repair/fallback. Source review found selected external values could be unbounded
inside Lua; an atomic HSTRLEN guard now limits copied values to 1 MiB, with original
HGETALL fallback per oversized row and exact permission compatibility. Native
boundary testing caught the ERR-token issue above. Independent final review found
no remaining actionable correctness/security findings; the measured concurrency
regressions remain explicit performance limitations requiring maintainer review.

All test/native processes and clients exited, owner-checked data/index clearing
completed, and a final empty/QPF/TLS/RESP3 gate passed before teardown.
[Cleanup proof](../../../../benchmarks/oltp-query-efficiency/2026-11-14/cleanup.json)
records owned container/network and generated environment/credentials removed,
with preexisting containers/networks/TLS files preserved. Operational artifacts,
identifiers, credentials and licenses remain private.

The independent CI race fix [PR #438](https://github.com/redis/redis-adbc-driver/pull/438)
merged with successful full Test and Release binaries workflows. That test-only
change is not a source of production performance improvement.
