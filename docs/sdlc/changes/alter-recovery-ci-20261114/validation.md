# Verification and review

[PR #439](https://github.com/redis/redis-adbc-driver/pull/439). November 14, 2026.
[Intent](intent.md), [spec](spec.md), [plan](plan.md).

The original [CI run](https://github.com/redis/redis-adbc-driver/actions/runs/43621781308)
failed only the amd64 driver job's TestSQLAlterCleanupResumes after its five-second
wait. arm64 driver and both snapshot/integration jobs passed. No review comment
or production corruption was reported. The local intermittent failure did not
reproduce in30unchangedrace repetitions; the claim is limited accordingly.

Verified owned Redis Software through `make -C go redis-up`, active/effective
4× QPF, six Search workers/mnp32,4GiB/noeviction, certificate-verified TLS/RESP3.
Before source `7b94a93ccb7bca367c01f0a73204d9cc91a236af`:

```sh
go test -v -race -count=30 -p 1 -timeout=10m -run '^TestSQLAlterCleanupResumes$' .
```

30PASS,zeroSKIP/FAIL, wrapper21.128s. Same source plus the test-only
worker-wake change in this PR:

```sh
go test -v -race -count=50 -p 1 -timeout=10m -run '^TestSQLAlterCleanupResumes$' .
```

50PASS,zeroSKIP/FAIL, wrapper44.065s. Both used snapshot URI unset,
ordinary URI targeting the verified disposable Software fixture. Private logs
retain complete output. Test file SHA256 at verification:
`a27542f65dfb1d8fb155e359b5ea0eecfab6d6ea840c14d3690ba5a0577cdb9e`.
Formatting and diff checks pass. Existing benchmark sources/artifacts remain frozen;
this test-only scheduling change makes no new performance claim.

An independent Codex subagent reviewed the diff: no actionable findings. The test
uses the actual reopened connection's worker and existing coalesced wake; no inline
collection, longerdeadline,SQLreplay,newskip or productionbehaviorchange. Original
first256/remaining344/retained600row and final generation/index/ownership checks
remain. This is scheduling control in a recovery test, not a new production GC
latency guarantee. Human maintainer approval remains separate. RemoteCIverification
is pending when this correction is pushed.
