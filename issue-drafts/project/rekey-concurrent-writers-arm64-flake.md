Target: redis/redis-adbc-driver — new issue
Title: CI flake: TestSQLRekeyConcurrentWriters intermittently fails on the hosted linux-arm64 Software cluster job
Labels: bug, needs-triage
---
## Problem

`TestSQLRekeyConcurrentWriters` (`go/rekey_integration_test.go:772`) runs continuous SQL writers against a paused rename/rekey copy. It waits for writer progress (`:849`) and stops the writers with 2 s timeouts (`:786-800`). It has failed intermittently in hosted CI:

- **#637:** attempt 1 of Actions run 45508137511 failed in job `linux-arm64-software-cluster-go-1`, step "Run complete ordinary Go test shard". Attempt 2 passed. #599's closing comment says CI went green "after rerunning one unrelated arm64 timing flake (`TestSQLRekeyConcurrentWriters`)".
- **Earlier:** a record mentions a "historical hosted stall" that a local pass "did not reproduce or resolve" (`docs/sdlc/changes/snapshot-parity/sustained-write-validation.md:36-38`).

Not verified: the failing assertion and its message, because the job log was not retrieved for this report. It is also unknown whether this is a test-timing artifact or a driver race.

Expected: the test passes deterministically.

Actual: intermittent failures on hosted arm64 that clear on rerun.

Versions: base `14fcd545` (#637); Go per `go/go.mod` (1.27.4); the Redis Software image from `go/compose.yaml`.

## Desired behavior

Find the failing assertion in the CI log. Then either fix the driver race, or make the test's synchronization deterministic, as was done for `TestSQLRekeyTakeoverWait` (`docs/sdlc/changes/rekey-takeover-ci-20261114/validation.md`). Avoid retries or longer sleeps that would hide a real stall.

## How to verify

```sh
cd go && bash software-setup.sh --exec go test -race -count=50 -p 1 -timeout=30m \
  -run '^TestSQLRekeyConcurrentWriters$' .
```

Success: zero failures locally, and the arm64 ordinary shards (`scripts/ci_go_suites.py run --suite ordinary …`) stay green across several hosted runs.
