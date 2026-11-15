# Validated point-read protocol: plan and verification

Date/source/requestor: [intent](intent.md); contract: [specification](spec.md). Status: implemented and measured candidate; human maintainer approval remains outstanding.

1. Inspected acquisition, manifest, metadata COW, descriptor, segment and row lookup sources. Complete.
2. Specified a bounded larger primitive with native ACL/declared-key gates and adversarial test protocol in the [proposal](../../../validated-point-protocol.md). No such upstream capability is claimed present.
3. Added opt-in partial SQL point attribution with Arrow type/result validation, independent physical HASH control, duration per pipeline shape, ordered standalone request times, and privacy/error-propagation regression.
4. Reproduced four baseline uncertain-absence failures: published/private paths accepted wrapped or joined Nil and read an older image. Implemented exact native-Nil validation, typed NoRetry, and one published ZCARD/ZSCORE pipeline. Private score-only, strict and found-row behavior retained.
5. Built baseline/candidate binaries before timing, captured RED/GREEN, froze source/binary hashes, then ran A1/B1/B2/A2 on exclusively owned Redis Software with verified active/effective 4x QPF. Complete; 448 measured queries plus 112 warmups, all full-fetch/type/result oracles passed. See [complete evidence](../../../../benchmarks/validated-point-protocol/README.md).
6. Candidate focused proof/privacy/metadata/lease checks: 40 PASS events including subtests, zero failures/skips. Focused Software visibility/corruption/strict-path checks: 27 PASS events including subtests, zero failures/skips. No full unit/race/cluster or public Python native benchmark is claimed.
7. Verified JSON/CSV accounting, local documentation links, source hashes, `gofmt` and `git diff --check`. Every task process/client/worker exited and fixture ownership was released before another agent's timing.

Request counts reproduce the saved round trips; end-to-end medians/tails and physical controls are mixed, so no general latency gain is claimed. The first standalone overlay `control_valid` accounts for most client-observed request time; its server CPU/cause remains unproven. No issue or external comment was created. Root agent source review found no blocker; this is not a human maintainer approval.

Independent review identified that the profile pre-acquires one execution transport for the full run. Per-statement pool acquisition/registration/finish/return/drain are excluded. This is now disclosed in source, all reports and corrected artifact metadata; original emitted labels remain preserved. Metadata/comment-only corrections after timing are identified separately in source provenance; runtime and test logic did not change.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
