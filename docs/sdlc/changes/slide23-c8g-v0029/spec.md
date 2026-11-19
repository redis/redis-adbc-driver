# Measurement contract

Date: November 18, 2026. Status: complete with recorded query limits. See [intent](intent.md).

- Official checksummed v0.0.29 Linux arm64 shared library and CLI; no rebuild.
- All 99,997,497 official rows and 105 typed columns, with matching dataset,
  schema and query hashes. Measure initial loading separately from query time.
- AWS c8g.metal-48xl: 192 vCPUs, 384 GiB RAM, 500-GiB gp2 EBS root disk, Linux.
  Verify actual placement, hardware and resources. Check a representative managed
  table's actual RAM footprint before committing to the full-scale RAM load.
- The user approved unmanaged mode after capacity pilots. Keep all 105 columns
  and native types; use an immutable fixture, three shards, eight ADBC ingestion
  workers, compact HASH values (4096-byte threshold) and the documented predicate
  indexes. Label this configuration as tuned and unmanaged. Managed snapshots
  are not disabled or represented as part of this run.
- Redis Software RAM with effective 4× QPF, six Search workers, mnp/32,
  noeviction, certificate/hostname-verified TLS and RESP3. Flex option false.
- Complete 43-query suite, three trials/query. Preserve first-resident-RAM and hot timings;
  compare the minimum of trials 2 and 3 with published same-machine hot results.
  Record execution/full Arrow fetch, independent reference validation, per-trial
  process memory/isolation limits and failures with null timings. A fixed
  300-second wall deadline applies to every trial. The client memory boundary
  is set once to 70% of the available memory after loading, sharing the physical
  machine with the resident Redis dataset and reserving the remainder.
- Resolve published same-machine entries from an immutable upstream commit and
  preserve source files, versions, dates, hashes and all 43 comparator cells.
  Matching hardware alone does not prove identical software/client configuration.
- An independent AWS cleanup principal and periodic expiry schedule bound runtime
  with a twelve-hour expiry checked every five minutes. Price verified through
  AWS Pricing API: $7.65696/hour for the RAM host, or $91.88 for a planned
  twelve-hour window, plus polling/termination grace and temporary EBS/storage.
- No other benchmark database or load competes with the timed Redis queries.
  Keep infrastructure identifiers, operational artifacts, credentials and license
  material private. No separate Flex backup remains in scope.

See [plan](plan.md).

## Explicit scope additions during the run

The user requested a separate test of latest code after pulling main. Pin that
source build to `b06aea4f4a9278120067d30a8657ed338680da0b`, retain the official
v0.0.29 failures, and compare query-specific outcomes under unchanged limits.
Main results cannot be merged into the release's timing array or presented as
a complete 43-query run. A safe pause preserves completed trials and their raw
records; resumed work must skip those identities without replay.

The user then explicitly requested a **full RAM database backup in private S3**.
This is separate from the cancelled Flex backup. Pause timed work at a complete
trial boundary, export all three Software shard RDBs, record exact S3 versions,
and verify source/stream/stored bytes and SHA-256 before marking the backup
complete. Keep database backup locations, manifests and operational details
private. Upload verification does not establish a successful restore.

Use GitHub issue [#487](https://github.com/redis/redis-adbc-driver/issues/487) to
track all ClickBench work and link the per-query issues there.


## Final full-suite contract (supersedes the partial latest-code retest)

After PR #497 merged, the user requested this chat complete its own full suite.
The final query library is main `14b4a4940e78bf5496a0532caf6b8e800d38dd2a`,
reported `v0.0.29+main.14b4a494`, natively built ARM64 with Go 1.27.4 through
`make build` and its version linker flag. It uses the same verified immutable
loaded RAM fixture, all rows/columns and the same fixed resource/deadline limits.
No new ingestion is performed; loading remains explicitly attributed to v0.0.29.
The official release, b06 diagnostics and final main measurements remain separate.

The user changed the trial policy during execution: when trial 1 hits 300
seconds, document the issue, skip trials 2 and 3, and continue with the next
canonical query. Skipped slots are explicit, with null timing, rather than extra
measured failures. Preserve repeats already underway before that instruction.
Successful first trials and memory-limit failures retain the three-trial
protocol. All failures are linked to labeled repository issues under #487.
Hot comparisons continue requiring valid trials 2 and 3; all 43 valid hot
results remain necessary for a full-suite score.
