# Validation

Date: 2026-11-20. Source base: `27e5a8d2`; local changes for [#554](https://github.com/redis/redis-adbc-driver/issues/554). Author: Codex.

A dedicated disposable Docker Redis Software fixture was created with `make -C go redis-up`. Docker's default address pool was exhausted; a private temporary Compose override selected a verified unused subnet. No existing networks/fixtures were removed. The setup and each integration invocation used the ownership/policy gate, verifying Software 8.8.0, three shards, TLS, noeviction and active 4x QPF (six Search workers, mnp routing, 32 connections). Operational artifacts remain private.

Checks from `go/` (integration commands run through `software-setup.sh --exec` after the above gate):

- `go test -count=1 -v -p 1 -timeout=10m -run '^TestLiveCollection' .`: two top-level tests and 15 subtests passed, no skips. Deterministic SCAN pages preserve real Software identity/index/HASH reads and prove first-fill completion after two HASH fetches while later source pages remain unread. An independent fixed-data oracle verifies complete int64 values, NULLs, residual expressions, binary-key inclusion, duplicate/nested/missing keys, empty pages, sparse filters, LIMIT/OFFSET and one-row/byte-budget batches. Lifecycle cases cover uncertain SCAN/HASH replies without replay, cancellation, early release, pre-scan blocking-plan/live-UNION rejection, permission refusal, initially exhausted results, changed definitions and deleted indexes before buffered output.
- `go test -count=1 -v -p 1 -timeout=10m -run '^(TestLive|TestStream|TestScanProjection|TestUnmanagedProjection)' .`: 137 pass events, seven expected skips (three opt-in profiles and four tests requiring the separately configured `REDIS_SNAPSHOT_URI`). Ordinary live SQL/Arrow tests exercise actual server SCAN, original data/index ownership and TTL preservation. No profile or benchmark was enabled.
- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`: passed across all packages; Redis integration skips are expected and establish no additional server compatibility.
- `gofmt` on changed Go files and `git diff --check`: passed.

The initial focused regression failed at the existing managed-only `stream_required` preflight, confirming the admission gap before it was extended. A test-hook reuse bug during development was fixed by scoping injected discovery to the owning execution; production iteration was unaffected.

Review checked full predicate rechecks, native exact values/NULLs, request bounds, live definition/index identity on every fetched/emitted page, source nonmutation, no-replay behavior, cancellation and Arrow cleanup. No performance, Cluster API, native C-boundary or Flex validation is claimed. Live HASH registrations retain their existing Flex exclusion. Human review remains pending.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
