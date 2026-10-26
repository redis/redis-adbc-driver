# RESP3-only plan and validation

Date: 2026-10-22. Source: `6508626`. Status: implemented and reviewed locally.
The accompanying pull request carries this change; no release or issue created.

1. Add URI rejection and connection fallback regressions.
2. Require RESP3 in client configuration and verify each new connection.
3. Replace dual-protocol driver integration coverage with explicit RESP3;
   update current guidance and benchmark options.
4. Run focused checks and inspect lifecycle, error and compatibility behavior.

## Evidence

- Before the fix, `env -u REDIS_URI go test -count=1 -p 1 -run '^TestRESP3' .`
  failed all eight URI rejection cases. Local socket checks initially skipped
  under sandbox restrictions; subsequent checks ran with localhost access.
- `make -C go redis-up`: passed; Redis Software, Redis 8.6.2, Search installed,
  one shard, active 4x QPF verified by the repository setup script.
- With `REDIS_URI=redis://localhost:6379/0`, ran
  `go test -count=1 -p 1 -run 'TestRESP3|TestClientURIOptions|TestClientURIClusterParamsConnect|TestTimeoutNotRetried' -v .`:
  passed, no skips. Includes all URI cases, rejected HELLO and protocol-2 HELLO
  replies for both endpoint types, successful SQL rows/count/schema changes,
  connection option propagation and timeout retry safety. The real fixture is
  single-endpoint; cluster rejection uses the local fake server, not a live
  cluster deployment.
- Full unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...` is not green:
  existing `TestACLReadme`, `TestACLAdopt`, `TestACLHashTools`, and `TestACLSweep`
  expect ACL text in README that is absent at the source commit. Integration
  skips are expected with REDIS_URI unset. A repeat excluding TestACLSweep still
  fails the other three unchanged README assertions. No assertions were weakened.
- `gofmt` applied; `git diff --check` passed.

## Review

Inspected propagation of OnConnect into cluster options, timeout replacement
clients and copied retained-source clients. Verification runs before application
commands; regressions confirm PING is never sent after failed negotiation.
Current documentation rejects RESP2; historical artifacts remain labeled as
originally measured. No data layout or ownership changes. No human approval or
release is inferred. The unrelated README test failures remain a validation gap.

See [intent](intent.md), [specification](spec.md).
