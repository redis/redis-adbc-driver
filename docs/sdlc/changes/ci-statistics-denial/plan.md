# Plan and verification

See [intent](intent.md) and [spec](spec.md). Date: 2026-10-22.
Source: `f9045da`. Author: Codex. Status: local verification complete; hosted CI pending.

1. Inspect failed workflow logs and compare with current main.
2. Reproduce TestStatisticsReadOnlyPermissions before changing its assertion.
3. Assert shared I/O status, Search NOPERM reason and absent result reader.
4. Run focused ACL/statistics checks, Software checks and unit-only suite.
5. Review against REVIEW.md and submit the fix for hosted CI.

Before the fix, the real OSS Cluster test failed with the same preserved
Search NOPERM error as CI. Current main's TestACLReadme already passes.
`make -C go redis-up` verified Redis Software 8.6.2, one shard, active QPF 4x.

## Verification

- OSS Cluster compatibility: `REDIS_URI=redis://localhost:7001/0 go test
  -count=1 -p 1 -v -run '^(TestACL.*|TestStatistics.*)$' .` passed all 15 tests
  in 2.262s, including the real statistics command denial.
- Verified 4x-QPF Software: the same focused command against the Software
  endpoint passed in 1.547s: two passed, 13 existing protocol-ACL cases skipped
  because Software manages users outside the Redis protocol.
- `env -u REDIS_URI go test -count=1 -p 1 ./...` passed all three packages;
  integration tests skip without REDIS_URI, so this is unit-only evidence.
- `gofmt` and `git diff --check` passed.

## Review

Agent review under REVIEW.md found no actionable correctness, ownership or
security findings. The assertion requires a nil reader and a specific Search
permission denial; arbitrary errors cannot satisfy it. Production behavior,
permissions, fixtures and skips remain unchanged. Existing documentation ACL
checks pass against the current driver reference. Historical failed runs remain
failed; a new PR run is needed to validate the hosted architecture matrix.
Human review remains pending.

## Follow-up: TLS resharding

The first hosted run of this PR cleared the Go failures and completed Software
successfully, then failed an independent TLS partition worker after resharding.
The child error was deliberately hidden, so its cause is not yet established.
Several local full TLS checker runs passed all 20 checks. Reusing the migrated
fixture also exposed a Search unavailable-slots error during setup; this is not
proof of the hosted worker's cause. A failed-job rerun is in progress.

Add fixed allowlisted worker error categories and exit status to failure
diagnostics, preserving private stderr. A focused adversarial test proves that
unknown errors and credential-bearing strings are never echoed. All 17 cluster
management helper tests pass. No retry, skip or production change is introduced.

The failed-job rerun reached the final exact read, but returned 1,098 rows,
missing only the migrated fixture row (id 0). All returned values and schema
matched. This demonstrates that administrative aggregate readiness alone is
insufficient to establish native Search readiness after this fixture migration.
The diagnostics-only full hosted matrix subsequently passed all eight server/
architecture jobs, confirming the intermittent nature of the remaining failure.

Strengthen the existing convergence gate with fresh restricted native probes.
Require three consecutive exact rounds; tolerate only missing expected rows and
the explicit unavailable-slots error during this bounded gate. Wrong values,
schema, unexpected/duplicate IDs and unrelated errors remain terminal. Keep the
separate final exact read without retries and all physical ownership checks.

Local final validation on a fresh owned three-primary TLS fixture: all 20 checks
passed, gate required seven rounds, final result had all 1,099 exact rows and no
missing/unexpected/duplicate/changed IDs. All 38 TLS management helper tests
passed, including native gate reset/deadline and corrupt-result rejection cases.
`git diff --check` passed. Review found no production behavior, ACL or ownership
changes; the fixture now proves readiness through the worker path it tests.
Hosted verification of this final gate change remains pending.
