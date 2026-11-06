# Distribution CLI managed-copy smoke

At `431dcb2`, all ten Linux distribution jobs pass their Python validation suite
then fail the `redis-adbc` smoke step. The applied import now prints
`Imported: … is a managed copy …`, while the workflow still requires the old
`Adopted: … now reads the HASHes …` result.
[The Ubuntu amd64 job](https://github.com/redis/redis-adbc-driver/actions/runs/42152094727/job/127679836831)
records the stale shell assertion and exit 1.

Keep the existing general-inspection checklist assertions: `check` still exposes
its existing legacy checklist title. Update only the applied-result assertion
to the exact managed-copy message. Retain the exact two-row copy result, require
the adopt report's two qualifying source rows, and verify that both source
HASHes still have exactly their original two fields and values. No release
platform or validation gate is removed.

A current locally built CLI passes this check/copy/adopt sequence against an
owned strict-TLS Redis Software deployment with effective 4x QPF, noeviction and
the ordinary aggregate cap. Both SQL tables contain exactly `('a',1),('b',2)`;
source HASHes remain unchanged. The catalog is initialized first, matching the
workflow's preceding ADBC suite. The owned tables and source keys are cleaned.

Existing CLI `TestCheckCommand`, `TestCopyCommand` and `TestAdoptCommand` all pass
under `-race` on the same owned Software fixture (root 2.684 seconds). These
retain row-count, private-layout and explicit-refresh assertions. Workflow YAML
parses and the extracted CLI shell block passes `bash -n`. Replacement hosted
distribution jobs remain the final packaging/platform gate.
