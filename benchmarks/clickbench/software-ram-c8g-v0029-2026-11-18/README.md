# Redis Software RAM / ADBC v0.0.29 on c8g.metal-48xl

Status: archived partial release baseline, November 18, 2026. All 99,997,497
rows were ingested; the release sweep stopped after 17 trials. The completed
[separate full main run](../software-ram-c8g-main-14b4a494-2026-11-18/README.md)
provides the final 43-query evidence and slide update.

The requested comparison covers all 43 ClickBench queries, three trials each,
using one AWS c8g.metal-48xl (192 vCPUs, 384 GiB RAM, 500-GiB gp2 EBS) and the
checksummed released Linux arm64 driver. The user approved unmanaged table mode
after capacity measurements showed the managed layout would exceed safe memory
capacity. All 99,997,497 rows and all 105 typed columns remain required.

The selected configuration is Redis Software RAM with three shards, verified
4× QPF (six Search workers, mnp routing, 32 connections), noeviction, TLS and
RESP3. Tuning uses `hash-max-listpack-value=4096` and predicate field indexes;
see the capacity pilot for the exact indexed column list. Eight independent
ADBC ingestion workers append new, disjoint explicit keys. The dataset is
immutable during queries. This unmanaged mode does not provide managed statement
snapshots. The released unmanaged path permits native integer NUMERIC candidate
narrowing but does not trust index copies for sorting or value aggregation;
those operations evaluate authoritative HASH values. These results must not be
presented as managed-mode aggregation performance.
The separate Flex backup was cancelled.

The full-load record reports 7,035.959 seconds for ingestion and
7,590.459 seconds from load start through readiness certification. The latter
includes the failed SQL verifier and subsequent independent certification;
these durations are distinct from query timings and source download time.
Redis used 262,259,371,888 bytes (262.3 GB / 244.2 GiB) after loading. This is
Redis's used-memory metric, which is not interchangeable with another entry's
compressed on-disk size or the host's nominal RAM allocation.

Post-load SQL `COUNT(*)` reported a snapshot-lease error in both the original
verifier and a fresh process. All eight disjoint workers acknowledged complete
input, the external index reports exactly 99,997,497 documents with zero indexing
failures, and a fresh SQL client verified all 105 schema types and all values in
the first, middle and last source rows. The SQL count failure is retained and
reproduced in all three timed Q1 trials; it is not a successful SQL count result.
Tracked in [issue #490](https://github.com/redis/redis-adbc-driver/issues/490).
Q2 matched the independent reference in all three trials. Q3 and Q4 reached
the 300-second deadline in all three trials. The release sweep paused safely after 17 completed trials for the user-requested
latest-code retest. Q6 also hit the deadline in two completed trials. Timeout
reports: [Q3 #493](https://github.com/redis/redis-adbc-driver/issues/493),
[Q4 #494](https://github.com/redis/redis-adbc-driver/issues/494),
[Q5 #495](https://github.com/redis/redis-adbc-driver/issues/495), and
[Q6 #496](https://github.com/redis/redis-adbc-driver/issues/496).
These observations do not establish a full-suite score.

The user requested a separate retest of pulled main
`b06aea4f4a9278120067d30a8657ed338680da0b`. It contains the snapshot heartbeat
change in PR #492. The latest published release remains v0.0.29. Latest-code
results must remain separately labeled, using the same loaded dataset, fixed
300-second deadline and memory cap; The b06 diagnostics stopped after eight trials: all three Q1 and Q2 results
matched the independent full-data references, and two Q3 trials reached the
deadline. Q1 times were 89.495, 87.813 and 88.749 seconds. These partial diagnostics
remain separate from both the release baseline and the final full main run.
The user-requested full RAM database backup was completed separately. The full **unmanaged RAM** backup is now complete in private S3: three shard
files totaling 68,144,059,084 bytes, with exact versions, byte counts and
SHA-256 readbacks verified. The final file used eight concurrent ranged reads,
preserving previously completed checks. Its folder, manifest and object
metadata explicitly identify unmanaged mode. The private manifest and object
locations remain outside this repository. **Restore testing has not been
performed.**

Overall tracking: [issue #487](https://github.com/redis/redis-adbc-driver/issues/487).

Capacity evidence contains three validated pilots. These estimates preceded
the completed full-scale ingestion: default managed first-100K rows, tuned
managed representative million rows, and tuned unmanaged representative million
rows. The latter two sample uniformly across up to 1,000 source row groups.
All pilots retained the full typed schema, checked the row count and compared
all 105 values in the first, middle and last sampled rows with the source.

`published/manifest.json` identifies 129 latest-per-entry published results with
the exact machine filename, pinned to ClickBench commit
`2b3c03476e801589683007ac9164341e481a6fc6`. Their original JSON bytes and SHA-256
hashes are retained. Of these, 118 contain all 43 finite, nonnegative hot results
for single-host CPU runs. Five complete entries report zero for some hot
queries; their values are preserved. Raw division by a zero timing is left
undefined, while the ClickBench 0.01-second offset ratio remains available.
The latest pinned PostgreSQL DDL and all 43 query texts also exactly match the
canonical schema and independent-reference SQLs used by this run.
Matching machine types does not establish identical
client, software, tuning, date or durability settings.

Run `python3 analyze.py` to verify the published source hashes and coverage.
Once the Redis run and independent correctness records are available, pass
`--run RUN.json --correctness CORRECTNESS.json` to produce the 43-query CSV and
comparisons against every entry. `per-query-comparisons.csv` also retains every
query/entry pair and computes ratios only when both hot results are available.
Hot timings require valid trials 2 and 3; the
minimum of those trials is used. Full-suite ratios require all 43 valid hot
results and use the ClickBench 0.01-second offset. Failed or incomplete results
receive no synthetic timing or overall ranking.

See the [measurement contract](../../../docs/sdlc/changes/slide23-c8g-v0029/spec.md).
Infrastructure identifiers, credentials, license material and operational
scripts remain outside the repository.
