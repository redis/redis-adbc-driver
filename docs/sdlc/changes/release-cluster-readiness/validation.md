# Validation and review

Date: 2026-10-20. Starting source: `11faf538`. Author/reviewer: Codex (agent
review, not human approval). [Intent](intent.md), [spec](spec.md), [plan](plan.md).

## Observed failures and limits of diagnosis

[Release validation attempt 1](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39469795156/attempts/1)
failed on linux-amd64 in TestACLDriverUser and TestACLClientName with CLUSTERDOWN,
then TestAdoptHashes/TestAdoptV0011Table returned partial results. linux-arm64 and
macOS passed. The independent eight-job Test matrix
[passed](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39469795134).

The fixture's old health check queried only node 7001. Early-only failures are
consistent with incomplete cluster convergence. The original run did not capture
per-node logs/topology, so the exact failing node/state is not proven. This fix
addresses the demonstrated false-positive health gate and adds diagnostics rather
than asserting an unobserved driver bug or weakening the failed assertions.

## Local verification

- Deterministic readiness regression: `python -m unittest discover -s scripts -p
  test_cluster_ready.py -v` passed all five scenarios: all ready, second/third
  primary CLUSTERDOWN, second primary unreachable, third primary missing Search.
  CRLF CLUSTER INFO output is covered. No Redis server needed for these cases.
- New disposable redis:8.6.2 cluster on ports 27001–27003, using the actual Compose
  service plus new health script: `docker compose ... up --detach --wait` passed.
- `REDIS_URI=redis://localhost:27001/0 GOCACHE=/private/tmp/redis-adbc-go-cache go test
  -count=1 -p 1 -run '^(TestACLDriverUser|TestACLClientName|TestAdoptHashes|TestAdoptV0011Table)$' ./`
  from go/: all four previously failing tests passed (1.090 seconds).
- Real fault injection: stopped only primary 27003 in that disposable container.
  Immediately afterward, the old seed-only check still returned healthy while
  the new all-primary gate failed with connection refused on 27003. The temporary
  container was then removed; existing user containers were untouched.
- `ruff check` and formatting for the regression test, `sh -n go/cluster-ready.sh`,
  YAML parsing of Compose/both workflows, and `git diff --check`: passed.

## CI and release follow-up

Reran only failed jobs of the original PR release-validation run, preserving its
first attempt. The independently published
[v0.0.15 release run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39470185455)
has now passed linux-amd64, linux-arm64 and macOS build/test jobs without changing
the release tag. Its ten Linux distribution jobs were still running at this
record's last check. Newly committed readiness changes require their own PR CI;
passing reruns of old source are not represented as validation of that diff.

## Review

Checked fail-closed behavior for every node, command-error propagation, CRLF,
read-only health probes, retained Compose timeouts, read-only mount resolution,
CI path triggers and failure diagnostics. No driver code, fixture assertions,
ACL recipes, release tags or test skips changed. Search availability is checked,
not merely TCP connectivity. The added diagnostics tolerate individual failed
probes so remaining topology information can still be collected.
