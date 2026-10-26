# Implementation and verification

Date: 2026-10-21. Status: verification in progress; human review pending.
See [intent](intent.md) and [spec](spec.md).

1. Preserve failing OSS/Software CI evidence in issue #393.
2. Add deterministic FT.INFO server errors to the existing real-ALTER hook test.
   Before the fix, all four new success/cancellation subtests failed with I/O.
3. Share narrow classification between bounded readiness and attribute discovery.
4. Check metadata publication, no mutation replay, cancellation, terminal errors,
   direct readiness status and unchanged concurrent ALTER assertions.
5. Run focused stress, race checks and the full configured Go suite. Record actual
   results below. The local cluster is disposable Redis 8.6.2, three primaries,
   macOS arm64 Docker host; Go 1.26.6. Hosted Software/amd64 checks remain CI evidence.

Initial focused cluster run: `go test -count=20 -run
'^(TestAlterWaitsBeforeMetadataCommit|TestSQLAlterMultiConcurrent|TestInconsistentIndexState|TestWaitIndexReady)$' .`
passed (15.994 s). Final focused cluster race run (including direct readiness status and no FT.ALTER
replay assertions): passed, 2.837 s. The same five focused tests on native macOS
Redis 8.10.2 / Search 8.10.1 passed 20 repetitions, 4.955 s. No feature skips in
these focused selections. The full cluster suite remains pending.

Review: inspected exact server-error matching, fail-closed partition behavior,
context handling and metadata ordering; no additional actionable finding. This
agent review is not maintainer approval. Hosted Software and Linux amd64 runs
must still validate this change; no Cloud or macOS TLS result is claimed.
