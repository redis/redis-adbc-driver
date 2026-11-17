# Current issue investigations

Source baseline: `3671fca7` (2026-11-17). Requestor: Albert Wong.
No production speedup is established by these investigations. See the
[change record](../../../../docs/sdlc/changes/issues-399-444-447/intent.md).

## Metadata and mutation observations (#444/#445)

[Full Go protocol records](go-profile.json.gz) and [summary](go-summary.json)
retain all 12 cases, each with one warmup and 32 validated measurements and an
independently checked complete final fixture. Foreground contexts and outside
operation/maintenance calls are kept separate. The source includes the local
bucket-error safety correction; it does not include a latency optimization.
The source scope and lack of frozen whole-test-binary attestation are recorded
in validation; these command counts are not a native performance qualification.

This is **macOS arm64 instrumented Go over a Docker published TLS port**, against
an owned Software 8.2.6 container / Redis 8.8 with Search enabled and three primaries,
active/effective 4× QPF, six workers, mnp/32 and noeviction. It ran while build work
was active. Elapsed samples are therefore **not an isolated native Linux timing
window or acceptance evidence**, and are not comparable to historical native
Python medians. Do not pool them with those measurements. Hook calls are not
packets, RTT counts or server CPU. Commands in a pipeline are counted separately.

Both point reads retain 10 median foreground hook invocations and 19 managed /
18 unmanaged commands. Managed INSERT/UPDATE/DELETE retain median 38/49/43 hooks
and 69.5/73/66 commands (fractional medians reflect paired integer observations).
These counts motivate investigation; they do not identify causal elapsed costs.
No cache candidate is adopted. Fresh root, bucket and object identities remain
dependent; a dynamic-key Lua traversal would violate declared-key constraints.
Any future batching must also retain native ACL/error precedence and bounded
materialization before it can compete on the original ≥10% gates.

A correctness prerequisite was reproduced: bucket proof resolution used
`errors.Is(err, redis.Nil)` and swallowed a joined missing-value/EOF result at
either pipeline or entry level. The new regression failed before the fix and
passes after it. The resolver now checks every proof reply and accepts only an
exact direct native missing entry; unknown/composed outcomes stop without fetching
an object or retaining a checked bucket. This adds no commands or storage format.
It is a safety fix, not evidence that #444/#445 latency targets have passed.

## Release ownership (#446)

The [ownership inventory and current safety checks](ownership.md) document seven
passing normal probe groups and the unchanged-policy race/checkptr abort.
Production GC policy stays unchanged; sanitizers and complete lifetime gates
remain outstanding. Earlier negative performance evidence remains applicable.

## Explicit no-result caller (#447)

The pinned manager already supplies `cursor.executemany(sql, None)`; the public
[caller example and matched comparison](../../README.md#explicit-no-result-caller-comparison-447)
need no DBAPI fork. [Native correctness checks](no-result-correctness.json) passed
both callers in both modes using the same frozen native Linux driver. All four
checks validate affected counts, parameters, NULL/exact int64/NUL values, no-op
writes, preceding SELECT cleanup, full SQL final states and matching error
status/SQLSTATE. Both unmanaged cases also validate original HASH keys/values.
Actual ACL denials and cancellation/Close acceptance remain separate gates.

These native checks use Linux aarch64 in the Software container's network
namespace, trusted TLS/RESP3 and the verified one-shard 4-GiB Software policy.
They are correctness screens, not timing records.

## Frozen native caller comparison

The [four windows](windows.json) ran once in A1/B1/B2/A2 order, with the same
native source/library/CLI and wrapper frozen before timing. All 672 trial records
passed complete matrix/value/state auditing, including the 168 warmups. The
[report](report.md), [full comparison](comparison.json), [CSV](comparison.csv),
[pair CSV](pairs.csv), [A1](A1.json.gz), [B1](B1.json.gz), [B2](B2.json.gz) and
[A2](A2.json.gz) retain every case and raw control. [Provenance](provenance.json)
records exact identities, route and limits. No window was repeated or discarded.

| One client | Operation | execute p50 ms | explicit no-result p50 ms | Less elapsed |
|---|---|---:|---:|---:|
| managed | insert | 11.1574 | 6.0354 | 45.91% |
| managed | update | 14.9823 | 6.8395 | 54.35% |
| managed | delete | 11.8586 | 5.9582 | 49.76% |
| unmanaged | insert | 2.7830 | 1.8762 | 32.58% |
| unmanaged | update | 3.4134 | 1.7765 | 47.95% |
| unmanaged | delete | 2.5691 | 1.9500 | 24.10% |

The predeclared pooled six-cell ≥10% numerical gate is MET, but the performance
conclusion is **inconclusive**. Raw one-client Redis DELETE/INSERT/point-read
medians also fell 34.32%/22.20%/19.10%, and four-client raw mutation controls
shifted by roughly 29–45%. The forward pair improves unchanged point reads by
about 49%, whereas the reverse pair changes them by only 0.1–0.3%. This exposes
strong window/order/environment drift. Do not normalize it away, attribute the
entire pooled change to the caller, or call this an accepted optimization.
Reverse managed INSERT is 3.6% slower and reverse managed DELETE improves only
4.1%; pooled managed DELETE p95 rises 2.31% and four-client managed UPDATE p95
rises 0.45%. All other cases and tails remain visible in the linked report.

Operation clocks retain the original preceding-result cleanup placement and
exclude each thread's final cursor/connection close (one final result per thread).
That boundary is identical across caller windows but does not establish a full
process-lifecycle saving. Separate instrumented close/result-construction spans
are diagnostic, not additive components of pooled native medians. Actual ACLs,
cancellation/Close, sustained memory and stronger control stability remain open
acceptance work. Existing public no-result API suffices; no upstream API request,
DBAPI fork, preparation cache, GC removal or shipping latency claim is selected.


The corrected-source [native correctness screen](no-result-correctness-fixed.json)
also passes all four caller/mode cases. Separate instrumented runs retain 48 trial
records and 1,536 operation observations per caller:
[execute trials](stages-execute-results.json.gz),
[execute operations](stages-execute-operations.json.gz),
[executemany trials](stages-executemany-results.json.gz),
[executemany operations](stages-executemany-operations.json.gz), and
[nested span summaries](native-stage-summary.json). All keep actual manager
callbacks and original cancellation arguments. Per measured one-client mutation
trial, execute constructs 32 result iterators and closes 31 preceding results;
executemany invokes 32 execute_update callbacks without those iterators. Its
prepare bypasses the blocking-call observer and stays inside the outer operation
span. These are diagnostics, not a preparation-cache or isolated GC comparison.
