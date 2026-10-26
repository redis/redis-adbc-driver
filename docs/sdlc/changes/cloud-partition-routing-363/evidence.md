# Cloud Pro sharded routing evidence

Date: 2026-10-21. Source: cebe808 (merged #407), with this change's checker
additions. Requestor/resource owner: Albert Wong. Agent: Codex. Human review
pending. See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[runner](../../../cloud-partition-validation.md) and
[deployment metadata](evidence/deployment.json).

## Deployment and routing scope

One new disposable Redis Cloud Pro RAM subscription and database in AWS
us-west-2: 0.1 GB, no replication/persistence, no eviction, verified TLS,
50,000 requested operations/second. The dry run listed Shards quantity 2 at
$0.198 per unit/hour and MinimumPrice $0.274/hour. These are estimate components,
not an invoice or an additive total. Live management readback confirmed two
backend primary shards; Redis 8.6.2 and Search 8.6.11. The real aggregate
cap was 2,147,483,648; tests planned with a verified lower bound of 137.
The owner is responsible for temporary service costs.

The first run used the single-endpoint proxy client with Cluster API disabled.
Then the owned database enabled OSS Cluster API and external endpoints. It
reported `cluster_enabled:1`, but CLUSTER SLOTS/SHARDS/NODES advertised **one
external proxy owner for all 16,384 slots**, despite two management-reported
backend shards. The tests distinguish these two counts. They do not claim
direct routing to two backend shard endpoints, direct backend row distribution,
failover, or resharding coverage.

Cloud API credentials stayed in parent memory via non-echoed input; workers
received their own restricted identity and explicit verified CA bundle through
stdin. Roles map only to the owned database. OS trust was not modified. Existing
shared/demo databases and ACL resources were not adopted or changed.

## Checker additions and correction

`--minimum-shards` checks fresh management readback before ACL creation.
`--cluster-mode` applies to both planner and independent workers; automatic
Cluster API discovery grants the worker CLUSTER SLOTS and COMMAND access.
`--minimum-cluster-primaries` independently checks advertised slot owners.
A requirement for multiple advertised owners cannot silently pass in proxy mode.
Cluster routing evidence requires a complete nonoverlapping slot map, exactly
1,099 distinct live fixture keys, a unique owner for each key and nonempty row
counts on each advertised primary. Full Arrow values/schema still come from
fresh restricted native workers after the planner closes.

The first Linux Cluster API attempt failed in the newly added evidence collector:
FT.SEARCH returned a RESP3 map, while the collector assumed a RESP2 array.
Native connection and partition planning had succeeded. All six temporary ACL
resources were removed and verified absent. A one-row temporary owned probe
confirmed the response shape and was deleted. The collector now explicitly
decodes both formats and preserves the same count/uniqueness/coverage assertions.
A dual-format regression test covers the correction. See
[initial result](evidence/initial-linux-cluster-auto.json),
[initial cleanup](evidence/initial-linux-cluster-auto-cleanup.json),
[sanitized connection probe](evidence/linux-cluster-admin-probe.json), and
[setup corrections](evidence/setup-corrections.json).

## Verification

- macOS arm64, Python 3.13.16, ADBC manager 1.12.0, Arrow 26.0.0, native library:
  **16 proxy TLS/ACL/lifecycle checks passed**; six principals/rules/roles deleted
  and absence verified. [Result](evidence/macos-proxy.json),
  [cleanup](evidence/macos-proxy-cleanup.json).
- Linux ARM64 installed wheel, Python 3.13.16, ADBC manager 1.12.0, Arrow
  26.0.0, no native override: **17 automatic Cluster API checks passed**;
  all six ACL resources deleted and absence verified. All 1,099 fixture keys
  map to the one advertised proxy owner across the complete slot map.
  [Result](evidence/linux-cluster-auto.json),
  [cleanup](evidence/linux-cluster-auto-cleanup.json).
- Final OSS native TLS regression: **15 checks passed**; owned server stopped
  and private fixture removed. [Result](evidence/oss-regression.json).
- Final management/routing fault tests: **17 passed**, including prior recovery,
  ownership, credential/redaction, task and cleanup tests plus topology, routing
  requirement, incomplete slot-map and RESP2/RESP3 checks.
  [Output](evidence/unit-tests.log).

The proxy run preceded the final collector/argument refinements. Deployment
metadata records hashes for the proxy run, failed initial cluster attempt and
final harness separately. The final OSS run and unit tests use the final source.
RESP3 remains the native default for both client paths. A focused
`TestClientURIOptions` run passed added default-protocol assertions and the
existing explicit RESP2 compatibility checks.
[Protocol evidence](evidence/resp3-default.json). Historical Flex reports now
label their old RESP2 measurements without implying a current RESP2 default.

Go runtime source is unchanged; the full Go/upstream Python suites and benchmarks
were not rerun. Prior full-suite Cloud evidence remains in
[#405](../cloud-pro-validation-363/evidence.md), and minimum-client macOS evidence
remains in [#407](../cloud-partition-acl-363/evidence.md).

## Cleanup and remaining scope

All three ACL runs verified their six created resources absent, including the
failed initial collector run. The owned database returned HTTP 404 at
22:17:28 UTC and the subscription at 22:18:56 UTC. The exact owned Linux client
container and private TLS fixture were removed; API credential sessions closed.
[Cleanup record](evidence/cleanup.json). No new paid validation resources remain.

Issue #363 stays
open: the complete Cloud platform/client matrix, direct backend-shard routing
where actually exposed, failure/resharding cases, and representative performance
measurements still need evidence. This small fixed correctness fixture is not
a throughput or latency benchmark.

Agent review followed REVIEW.md for correctness/lifecycle, ownership/TLS and evidence scope. No actionable findings remain from that review. Human review and new-PR hosted checks remain pending.
