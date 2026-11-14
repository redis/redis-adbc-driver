# Specification

Date: November 13, 2026. Author: Codex; requestor: repository user. Human review pending. [Intent](intent.md), [plan](plan.md), [PR #436](https://github.com/redis/redis-adbc-driver/pull/436).

Use an exclusively owned Redis Software fixture, verified effective 4x QPF, six workers, mnp/32, 4-GiB noeviction shard, 4-CPU/8-GiB container and strict data TLS/RESP3. Reuse the exact ca685806 build from the prior 156-trial comparison. Synthetic fixtures are four typed columns: 1K point rows and 100K OLAP rows. Known-address point operations use persistent per-thread clients and one/four clients. Retain a warmup plus three measured rounds, 32 point operations or eight queries per trial. Queries cover filtered typed projection, filtered COUNT/SUM and grouped COUNT/SUM with exact integer oracles. No retries or multi-statement transaction claim.

Independent whole-state/output validation, setup and connection teardown stay outside timing. Record every raw operation latency, elapsed window, p50/p95 and closed-loop throughput. Preserve failures as failures. Full chart export rejects missing/duplicate/failed trials, wrong arithmetic and mixed driver builds. Keep historical ClickBench outside current-client series. Keep credentials/endpoints/infrastructure identities and private logs out of public evidence.
