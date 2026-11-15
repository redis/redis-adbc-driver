# Point-validation response isolation: plan and verification

Source/requestor: [intent](intent.md); contract: [spec](spec.md).

1. Read production segment validation, native no-retry wrappers, prior request
   attribution and its scope correction. Complete.
2. Add a separate opt-in diagnostic; preserve all earlier source/evidence. Complete.
3. Freeze persistent binaries and exact source hashes before measurement.
   Complete for both isolation versions, the width pair, response routing and
   standalone Go reproduction. All post-run source/binary hash checks passed.
4. Run the bounded diagnostic after the Software/QPF gate, require all native and
   ordinary SQL oracles, then close all processes/clients before slot release.
5. Retain complete sanitized samples and descriptive summaries; identify the
   implicated request/response boundary without claiming a causal server mechanism.
6. Proposed a maximum-two-segment grouping candidate, with explicit typed
   NoRetry, queue-error and later unknown/cancellation tests. New-contract RED
   cases failed on the baseline; this is not evidence of an old wrong-row/replay
   bug because the old FT.INFO marker already protected whole-pipeline retries.
   Focused race units passed (27 pass events, one endpoint-dependent skip).
   Live corruption/point/proof checks passed; an overbroad new test observer
   incorrectly inspected unrelated metadata ZCARD markers. That failed check is
   retained; the observer was scoped before timing and its focused live rerun
   passed. Neither failure was hidden or reclassified as a runtime speedup.
7. Built both matched persistent width binaries before A1/B1/B2/A2, with no
   intervening source/build work. All 256 samples and complete SQL/type oracles
   passed. Wide eight-pending-probe timing worsened to approximately 68 ms from
   17–19 ms; rejected the candidate and restored runtime and original assertions
   byte-for-byte. Complete patch/source/check/raw evidence remains archived.
8. Response-routing diagnostic passed 640 sequences / 3,840 fresh PING calls;
   same-client/pipeline-pool routing and the existing pool opt-out did not remove
   the response wait after a large GET. Standalone Go reproduced it in all
   448 sequences / 2,688 PING calls without ADBC, SQL, Search or Lua. No runtime
   optimization or native Python/ADBC latency improvement is selected.
9. Audited complete grids, native command counts, source hashes, supplemental
   links and rejected-patch applicability. Offline routing corruption tests pass.
   Coordinator independent client/topology controls and final integration checks
   are recorded separately; their causes must not be inferred from helper timing.
10. Coordinator independent RedisPy raw held connections reproduced the host
    route in 224 validated sequences. The exact same Go source on a Linux client
    in the unchanged Software server network namespace passed 448 sequences in
    0.36 seconds, first PING medians approximately 0.017 ms and every first sample
    below 0.1 ms. Client OS and network route changed together: this identifies a
    topology confound, not a driver/client/server algorithm cause. Exact source,
    selected resources, complete data, cleanup and binary hashes are retained.

All task-owned clients/processes closed and independent fixture verification
observed empty after each exclusive slot. No external issue/comment or PR was
created by this subagent. Human maintainer approval is not inferred. See the
[complete evidence](../../../../benchmarks/point-valid-isolation/README.md).
