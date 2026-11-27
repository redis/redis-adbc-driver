# Plan

2026-11-27; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Read the retained `duckdb-validation.json` records for Q72 and Q78, but not their pickles. Map their fields onto the `verified_objects` and `_artifact_error` paths.
2. Add regressions to `benchmarks/olap-release/test_isolation.py` and confirm they fail on unmodified main:
   - `test_validator_scratch_never_uses_the_shared_artifact_directory`: a synthetic writer empties every unsynced file under the artifact directory, while fsynced engine files stay intact;
   - `test_coordinator_gives_the_validator_local_scratch_outside_the_profile`;
   - `test_artifact_problem_separates_the_decoders_bytes_from_a_later_reread`, with the Q72 (zeros) and Q78 (recorded bytes) shapes;
   - the existing zero-filled-result test, extended to check `bytes_consumed`, with its re-read wording updated.
3. In `isolation.py` and `run.py`, implement the local scratch directory, the `--scratch` argument, `validation_scratch_filesystem` and the decoder-consumed fields.
4. Update the artifact paragraph in `benchmarks/olap-release/README.md`.
5. Run `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'` locally, and in the release client image on Linux with Python 3.14.10.
6. In the release client image on this Docker Desktop host:
   - stress the scratch write/close/reopen pattern on a bind mount and on the container's `/tmp`;
   - add fsync and same-handle variants, then an fsync read-back delay sweep;
   - run the coordinator with synthetic engines and a bind-mounted profile.
7. Open a PR. No merge, release or human review approval is inferred.

A live Redis smoke run is not part of this change: no Redis code path changed. Any later live check uses a Redis Software Flex fixture with verified 4x QPF.
