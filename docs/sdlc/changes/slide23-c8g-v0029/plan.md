# Execution plan

Date: November 18, 2026. Status: complete with recorded query limits. See [intent](intent.md) and [spec](spec.md).

1. Preserve the user's final scope: full 43-query RAM run, matching published
   c8g.metal-48xl hardware, no separate Flex backup. Terminate the cancelled
   task-owned Flex host without disturbing the RAM host or cleanup controller.
2. Provision/check c8g.metal-48xl, published driver artifacts and the exact source.
   Use `make -C go redis-up` and verify effective Software/QPF and policy.
3. Measure actual managed-table memory on a representative source fixture; assess
   full-scale RAM feasibility. The managed pilot exceeded safe capacity; the
   user approved the supported unmanaged mode. Its representative million-row
   pilot validated all typed sample values and projected approximately 266 GB.
   Use three Software shards and eight concurrent ADBC ingestion workers, then
   perform the measured full-scale load and
   validate row count, indexes and independent complete typed source samples.
4. Run all 43 queries, three isolated trials each; retain outputs, resource data,
   failures and independent typed-reference/tie validation. Do not fabricate an
   overall score from partial, failed or mismatched runs.
5. Resolve/preserve same-machine published comparison entries and their provenance.
   Calculate all valid per-query hot ratios and an overall score only if valid.
6. Update slide 23 in place with the audited result, methodology and evidence;
   verify numeric readback and rendered layout, preserving unrelated slides.
7. Retain reviewed benchmark evidence and remove task-owned runtime resources,
   access and schedules after exact ownership checks. No Flex backup is required.

## Historical release-run progress

- Three-shard Software RAM provisioned through `make -C go redis-up`; active
  factor 4, six Search workers, mnp/32, TLS/RESP3 and noeviction verified.
- Full ingestion completed through the checksummed released ADBC driver: all
  99,997,497 rows and 105 columns, with eight disjoint worker acknowledgements,
  exact index cardinality and independent typed source probes. Post-load SQL
  COUNT failed in two fresh verifiers; preserve that failure separately from
  the successful source-data checks. No uncertain input replay.
- The full query sweep is running. Q1 reproduced the lease failure in all three
  trials ([issue #490](https://github.com/redis/redis-adbc-driver/issues/490));
  Q2 matched the independent reference in all three trials. Q3 and Q4 each
  reached the 300-second cap in all three trials. Overall scoring is withheld.
- Published comparison coverage audit passed: 129 source hashes and shapes
  checked; 118 complete, single-host CPU hot series including reported zero timings
  (113 have strictly positive hot timings).
- Full-source/index readiness checks gate all query timings. The query runner
  attempts 43 × 3 isolated trials with a recorded 300-second deadline per trial,
  then independently validates outputs and LIMIT ties.

## Historical b06 latest-code retest requested during execution

- Pulled origin/main to `b06aea4f4a9278120067d30a8657ed338680da0b`, preserving
  local evidence. The latest published release remains v0.0.29; the user then
  explicitly requested testing the latest code.
- Paused the release supervisor at a persisted boundary after 17 completed
  trials, without interrupting a query. Q3–Q5 each have three deadline failures;
  Q6 has two. Created bug/needs-triage issues #493–#496 with exact evidence.
- Build the pinned main commit separately, verify effective QPF, source/schema
  and full-index readiness, then run Q3–Q6 three times under the unchanged
  resource/deadline contract. Include Q1 as a lease diagnostic and Q2 as the
  successful control. Validate complete outputs against the independent full
  references; keep release and latest-code results separate.


## Final completion

- A separate full-suite main `14b4a494` run covered all 43 queries, following
  the updated first-trial timeout skip policy. It executed 69 trials and retained
  all 129 slot classifications: 33 valid outputs, 33 measured timeouts,
  three memory-limit failures, and 60 explicit skips.
- Eleven queries passed all three independent DuckDB reference checks. Thirty-one
  queries hit 300-second limits; Q33 hit the client memory boundary. All 32
  failing queries have labeled GitHub reports; no output mismatch or unresolved
  independent oracle remains. Full-suite Redis scoring is correctly withheld.
- Retained the full 43-query matrix against all 129 pinned published same-machine
  entries, of which 118 have complete single-host CPU hot series. Source hashes
  and zero timings were preserved; no missing latency was imputed.
- Updated native slide 23, retained the requested slide deletion, reconciled
  related counters/summary wording, and inspected all 28 slide renders. A single
  consolidated repair fixed stale historical descriptions; the other 25 slide
  images were byte-identical after that repair.
- The full 68.14-GB unmanaged RAM backup is stored in private S3, version- and
  SHA-256-verified, without a restore-test claim. Results and private sources
  were retained before cleanup. Zero task-owned compute/volumes remain, and
  associated roles, security group and expiry controller were removed.

See [final evidence](../../../../benchmarks/clickbench/software-ram-c8g-main-14b4a494-2026-11-18/README.md).
