# Implementation and verification

Linked [intent](intent.md), [spec](spec.md). 2026-10-21.

1. Preserve original timing, correctness and OOM evidence; snapshot surviving
   shards before rewriting lost data. Reconstruct original parallel batch order
   using surviving anchors and preserve the missing shard's source rows in S3.
2. Add scan completion/diagnostic and backup health regression checks. Fix the
   supervisor gates, node-specific backup errors and RSS reporting.
3. Restore projection/sort inputs for parallel top-K; test hidden sort keys and
   SELECT *. Bound unrestricted row-ID sorts in both public scan paths.
4. Run Python helpers and Go unit tests, then focused integration tests on a
   disposable Redis, independently of the benchmark cluster. Build a new binary
   in a separate source directory and record its digest before live reruns.
5. Recover the failed primary, verify exact recovered data, rerun affected SQL
   and supplemental APIs with independent correctness checks, then complete the
   S3 backup, approved restore test and owned-resource cleanup. Do not declare
   the incident fixed until those operational checks pass.

Evidence: see [validation](validation.md). Python discovery 95 tests, 92 passed /
3 Linux-only skips on macOS; Go unit-only packages and the full native Linux
driver integration package passed. Live recovery and full rerun remain gated
and in progress; they are not certified by disposable integration tests.


2026-10-21 scope update from Albert: rerun only incorrect, failed or mismatched queries. Stop the full fixed sweep and preserve its completed trials. Run Q24/Q25/Q27 three times each (nine trials), independently validate all nine outputs, then rerun the previously failed 18 supported-scan measurements. Retain original full evidence separately; do not claim a complete fixed-binary sweep or mix binary timings into a full score. Complete backup, approved restore checks and cleanup remain required.


Scan timeout recovery: a full-data statement scan reached its1,200-second cap while making progress without memory pressure. Preserve that failed attempt and partial Arrow output, let its already-started partition partner finish unchanged, then resume only missing/failed measurements with a1,800-second cap. Verify source/binary/case identity on resume, retain successful measurements and independently revalidate each pair. The overall scan deadline and backup/restore reserve are unchanged.
