# Managed publication readiness diagnostic — November 14, 2026

**NOT SELECTED for integration.** The later [native subset](../2026-11-14-native/report.md)
remained mixed, including a 30.3% four-client DELETE p50 regression, so publication
batching was omitted while the separately demonstrated safety fixes were retained.

These are instrumented Go SQL diagnostics, **not native Python/C-boundary
latency measurements or Redis server CPU**. All 384 observations are retained in
[run 1](1-baseline.json), [run 2](2-candidate.json), [run 3](3-candidate.json) and
[run 4](4-baseline.json). Each run executed 32 INSERTs, 32 UPDATEs and 32 DELETEs
on matching 1K-row/four-public-column tables (plus one ordinal control field).
Every mutation's affected count and complete final values/schema width
were independently checked by `TestManagedKnownRowWriteProfile`.

The order was baseline/candidate/candidate/baseline, using frozen precompiled Go
test binaries; no competing builds or live tasks ran during these windows.
Both builds use production background maintenance on its separate unhooked
client. All operation samples are included, with no retry or discard.

| Operation | Baseline p50 ms | Candidate p50 ms | Baseline/candidate | Baseline p95 ms | Candidate p95 ms |
|---|---:|---:|---:|---:|---:|
| INSERT | 39.661 | 43.188 | 0.918× | 133.483 | 166.359 |
| UPDATE | 43.431 | 47.160 | 0.921× | 115.301 | 138.379 |
| DELETE | 43.808 | 41.539 | 1.055× | 127.186 | 131.481 |

Each pooled operation/build has 64 observations. Exact per-run/per-operation
p50s (INSERT / UPDATE / DELETE) were 42.835 / 44.093 / 45.535 ms, then
40.277 / 48.758 / 45.118 ms, then 43.614 / 45.632 / 39.637 ms, then
37.492 / 42.267 / 43.200 ms. **INSERT and UPDATE p50s are slower by 8.9% and 8.6%;
DELETE p50 is lower by 5.2%, but all p95s are higher.** No general latency speedup
is established. Background compaction/admission and finite sequential trials
prevent causal attribution. The coordinator's uninstrumented native subset and
full combined comparison decide whether to retain this performance candidate.

## Exact client exchange change

Every one of the 192 baseline mutations recorded one standalone FT.INFO and two
standalone ZCARD exchanges at final indexing/readiness. Every candidate mutation
recorded one standalone ZCARD plus one new `zcard,ft.info` read pipeline. Both
native reads remain; this is one fewer sequential client exchange per output,
not elimination of server work or proof of lower total statement latency.
First-ready outputs still perform the separate indexed-row query proof.

The helper selects the same primary as native `searchDo`, preserves FT.INFO
NoRetry and metadata-directory error precedence, and rejects whole-pipeline
unknown outcomes. Only exact direct native transient errors allow a later
fresh FT.INFO poll. Final ownership, lease/pin, cardinality, prefix, warnings,
publication and independent value checks remain.

## Sources and limitations

[Provenance](provenance.json) records actual source/test-binary hashes, execution
order, settings and complete observations. Baseline is PR #439 source
`7b94a93ccb7bca367c01f0a73204d9cc91a236af`; diagnostic candidate is
`b5e71b05` (full ID in provenance). Redis Software with verified active/effective
4× QPF, six Search workers/mnp32, noeviction, verified data TLS/RESP3; diagnostic
GOMAXPROCS=4. Operational endpoint/credential/license identities remain private.
This agent did not record server CPU or capture a standalone full environment
inventory; the coordinator's new native comparison must retain actual server and
host/resource versions before a broader performance claim.

[Checks](checks.json) retain successful and deliberately failing unit checkpoints.
The first focused verified Software race selection passed 18 top-level tests with
zero skips/failures; unit-only checks intentionally skip native integration.
A later parent review found two existing error-classification gaps in the same
publication method: joined/wrapped NOPERM query errors could trigger replacement
CREATE after an unknown read; joined FT.INFO failures were mislabeled Unauthorized.
A custom future pipeline queue error could also be ignored by the new helper.
Three focused regression groups fail on the predecessor, then pass after direct
native classification and checked queue errors. An initial patch assertion left
source unchanged, producing a second retained RED run despite its early log name.
The final corrected unit race selection passes seven tests with three explicit
native-fixture skips. Native compatibility must be rechecked in the next slot.

Those correctness guards were added **after** this diagnostic candidate froze.
These timings are not relabeled as the later safety-fix source. No new ClickBench,
Flex, transfer/bulk ingestion, Cluster API topology or full product ranking.
All four diagnostic processes and clients exited before the shared live slot
returned to the coordinator. Final fixture teardown is coordinator-owned and is
not claimed by this per-agent diagnostic record.
