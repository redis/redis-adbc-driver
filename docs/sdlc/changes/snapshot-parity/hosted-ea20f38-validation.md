# Hosted Software matrix and required Ray TLS coverage

Date: 2026-11-03. Hosted source ea20f38; local CI-gate parent caae4e8.
Production remains protocol 15. Full [parity](plan.md) remains incomplete.

## Terminal hosted result

[Test run 41788774439](https://github.com/redis/redis-adbc-driver/actions/runs/41788774439)
was manually dispatched for ea20f38. Helpers and coverage jobs succeeded. Both
Linux AMD64/ARM64 Software jobs failed at Go tests. Their successful setup
verified Redis Software, Redis 8.6.2, three shards, noeviction and active effective
4x QPF. The job name alone does not prove Cluster API, failover or persistence
coverage; managed rows still share one metadata hash slot.

The [sanitized inventory](hosted-ea20f38-inventory.json) records failed Go function
names and integration summaries. Raw job logs and operational artifacts remain
private. Failure events include parents, children and cascades; they are not
independent product-defect counts.

| Step | AMD64 | ARM64 |
|---|---|---|
| Python analytics integrations | 104 passed, 47 skipped | 104 passed, 47 skipped |
| Ray worker datasource | 16 passed, 1 skipped, 3 warnings | Not selected |
| Airflow | 5 passed | Not selected |
| dlt | 12 passed | 12 passed |
| Superset | 5 passed, 1 skipped | Not selected |
| Go | 350 failure events across 254 function names | 142 failure events across 97 function names; package timeout |

Both Go jobs first fail TestACLAppIndexReader, followed by the legacy DROP/RENAME
fixtures and foreign-index setup. The DROP/RENAME fixtures were restored later
on macOS in caae4e8; that does not make this earlier hosted run green or validate
the repaired cases on Linux. ARM64 hit the Go package timeout while
TestSQLRowValueScale was running; this does not establish an isolated root cause
or a row-value performance result. Later Go cases did not finish on ARM64.

GitHub step records confirm Python ADBC validation and restricted native TLS
partition checks were skipped after the Go failures on both architectures.
The 47 analytics skips and one Superset skip are not treated as feature passes;
the summary output does not enumerate all their reasons. No complete platform,
ADBC-validation or all-interface parity claim follows from this run.

## Ray TLS coverage gap and correction

The Ray module's sole per-case skip requires explicit db_options for its private
CA test. Its fixture reads REDIS_TLS_CA_FILE, but the workflow supplied only
SSL_CERT_FILE from Software setup. The other sixteen cases can use global trust
while the worker trust/rejection case silently skips.

The workflow now requires a nonempty existing SSL_CERT_FILE, exports it as
REDIS_TLS_CA_FILE for the Ray command and sets REQUIRE_RAY_TLS=1. Missing fixture
trust must fail rather than skip. No insecure TLS option, new secret or remote
credential is introduced.

Local validation uses the same environment contract on the owned strict-TLS
Redis Software fixture, after make -C go redis-up reverified active effective
4x QPF/noeviction. The versioned native library was rebuilt with make -C go
build. All 17 Ray datasource cases pass in 14.21 seconds with three upstream
warnings and zero skips, including worker success with CA and rejection without
trust. This is macOS ARM64 evidence; the workflow change needs a hosted rerun.

CI matrix helper tests: five passed. Workflow YAML parsing, Ray shell syntax and
diff whitespace checks pass. No ClickBench load/run or elapsed-time performance
comparison was performed.

## Review and remaining work

Author review covers exact hosted source/job scope, actual skip/failure counts,
timeout interpretation, trust propagation and fail-closed fixture setup. It is
not independent maintainer approval. Full ordinary, current Cluster/platform/
recovery, restricted reader ACL integration, process resume, sustained compaction
and broader comparative performance remain open. The protocol-16 reader design
is not activated.
