# Test hooks on OSS Cluster API node clients

Date: 2026-11-24. Author: Claude (Claude Code); requestor: Albert Wong, from
the #671 validation session. Source: `redis/main` at 268ccf03. Status:
implemented, local verification complete, review pending.
PR: [#672](https://github.com/redis/redis-adbc-driver/pull/672).
See [spec](spec.md) and [plan](plan.md).

While validating #671 (go-redis v9.24.0, issue #650) on OSS Cluster API
endpoints, `TestSQLSnapshotCleanupDeniedRecovery` and
`TestSQLAlterLeftoverAttribute` failed on both Redis Software with
`oss_cluster` on and a Redis Open Source 8.6.2 TLS cluster. They fail the same
way with go-redis v9.23.0, so #671 did not cause them. CI runs only the
single-endpoint Software proxy, where they pass.

Both inject a fault through a hook passed to the test harness's `rawClient`.
On a cluster the harness adds that hook only to the `ClusterClient`. The driver
sends Search commands, and go-redis sends WATCH … EXEC, to a node's client
directly (`store.search`, `newClient`), so the hook never sees them. The same
gap makes 15 of the 54 tests that pass hooks to `rawClient` fail on a cluster.

Fix the harness so a test hook sees every command once on a cluster, as on a
single endpoint. This changes test infrastructure only, not driver behaviour,
assertions or skips. Adding a Cluster API CI job is out of scope.
