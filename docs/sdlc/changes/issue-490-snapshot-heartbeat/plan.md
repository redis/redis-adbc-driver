# Plan and evidence: issue #490 heartbeat recovery

Date: 2026-11-18. Source: `06302c39`. Author: Codex. Requestor: Albert Wong.
Status: local verification complete; full-scale validation outstanding. Approval: no maintainer decision recorded.
Issue: [#490](https://github.com/redis/redis-adbc-driver/issues/490).
Inputs: [intent](intent.md), [specification](spec.md).

## Files and order of work

1. Trace `go/snapshot.go` heartbeat, foreground proofs and unmanaged aggregate execution.
2. Separate the heartbeat's ticker from its loop for deterministic fault injection.
3. Capture regression failures before changing error handling.
4. Require server validation before the next scheduled renewal following an unknown reply.
5. Add unit and Software regression coverage and document the contract in
   [snapshots](../../../snapshots.md).
6. Run focused race checks and the unit suite; review correctness, ownership and evidence.

## Verification evidence

- Before the fix, all three uncertain-error cases and the expired-pin regression
  fail because the heartbeat immediately abandons the pin.
- After the fix, the initial focused heartbeat tests pass, including the Software
  unmanaged count with renewal loss before and after execution.
- Fixture: disposable three-shard Redis Software, Redis 8.8.0, strict TLS/RESP3,
  active 4x QPF and effective Search configuration verified by `make -C go redis-up`
  and rechecked through `software-setup.sh --exec` before integration tests.
- Client: macOS arm64 Go tests through a Docker-published loopback endpoint.
  These are correctness checks, not benchmark latencies.
- Unit suite: `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -timeout=30m ./...` passed. Integration skips are expected in this unit-only pass.
- Software race pass: after `software-setup.sh --exec` policy/QPF verification,
  set `REDIS_SNAPSHOT_URI` to the verified disposable `REDIS_URI` and ran
  `go test -race -count=1 -p 1 -timeout=10m -v -run
  '^(TestSnapshotHeartbeat|TestSnapshotPin|TestSnapshotControlScript|TestUnmanagedAggregateInputVectors|TestUnmanagedLiveSourceWrites)' .`.
  Passed, including both lost-renewal SQL count cases, expired-pin and unavailable
  proof cases, native refusal, cancellation, normal/concurrent release and failed
  release recovery. No race reports.
- Two existing operator-ACL tests skipped: `TestSnapshotControlScriptEvalOnlySoftware`
  and `TestSnapshotControlScriptPointLeaseSoftware` need separately configured
  operator fixtures; these capabilities were not validated in this run.
- `gofmt` applied; `git diff --check` passed. Local relative documentation links checked.

## Review decision

Codex self-review against [REVIEW.md](../../../../REVIEW.md): no actionable findings
in the final diff. Correctness/lifecycle: foreground and server expiry proofs are
unchanged; uncertainty cannot bypass validation or revive expired pins; successful
and failed-release behavior remains covered. Security/ownership: no source writes,
index changes, new ACL requirements, storage changes or published operational data.
Intent/evidence: records distinguish injected correctness checks from the original
full-scale report and record operator skips. This is agent self-review, not human
maintainer approval.

## Limits

The full-scale v0.0.29 Linux arm64 reproducer has not been rerun. Fault injection
establishes the premature local lease-loss mechanism, not the original server or
transport cause. No ClickBench timing or 100-million-row loading was performed.
Operational logs and fixture identifiers remain outside tracked artifacts.
