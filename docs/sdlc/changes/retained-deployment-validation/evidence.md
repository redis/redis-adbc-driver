# Evidence

Status: complete for #363 on the revisions and deployment scopes below. The
runtime full Go/native/TLS results are in
[the runtime record](../retained-execution-budget/evidence.md#main-integration).

Local three-primary Redis 8.6.2 / Search 8.6.0, verified TLS: the expanded
checker passed 20 top-level checks. Retained checks cover global SQL, typed replay
after source DROP, tenant ACL denial, cancellation/release without revocation,
ASK/MOVED, denied ASKING, active-migration publication rejection and explicit
revocation. Both persistent worker connections replayed identical Arrow values
and schema. The migrated slot held 17 keys, all in this run's retained namespace.
Source tables, namespaces and newly created ACL principals were removed.

The checker initially expected the native ASKING-denial error to contain NOPERM;
the existing routing path can normalize that error. Keep the original native
error assertion, prove ASKING denial directly on every primary, and require
exact successful replay before denial and after restoring permission. Cross-tenant
GETRANGE denial still requires NOPERM.

Cloud: the owned Pro fixture was created for this validation. The dry run quoted a
$0.274/hour subscription minimum and a $0.198/hour shard line item; these are
quoted components, not a final invoice. The owned fixture existed for approximately 42 minutes. Database/subscription
deletion and absence verification completed after validation.
The API required an explicit network definition and rejected shard-count sizing
for a bundled Redis 8 database; planning now uses operations-per-second sizing.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Standalone Redis 8.10.2 / Search 8.10.1 verified TLS: all 16 checks passed,
including the retained subset. [Standalone report](evidence/standalone-tls.json),
[Cluster report](evidence/cluster-tls.json). These use the current native library,
not a newly installed wheel.

All four newly installed macOS ARM64 wheel cells passed: Python 3.13/3.14
with ADBC managers 1.8.0/1.12.0, 16 checks each including retained minimal ACL,
global SQL, lifecycle and replay. The per-cell JSON and
[wheel provenance](evidence/macos-wheel-provenance.json) record exact versions
and library hashes. Management helper fault tests: 33 passed.

[Refreshed current-runtime measurements](../../../../benchmarks/partitions/retained-main-2026-10-21/README.md)
contain 90 exact ClickBench reads and 168 exact synthetic reads.

Redis Software filters CONFIG GET maxmemory-policy even for the fixture's
administrator. [Observed rejection](evidence/software-config-filter-before.log).
The owned three-shard fixture's management API independently reports noeviction
([verification](evidence/software-policy-verification.json)). Test options now
accept an explicit environment declaration only after verified fixture setup;
Software setup checks the actual policy, including on reuse. Managed TLS
checkers freshly read policy through their management API and reject unsafe
policies before using `partition_control_persistence=guaranteed`. Driver defaults
and automatic checks are unchanged. Full Software validation passed: all three Go packages; native ADBC
**380 passed, 8 feature skips, 14 required partition cases passed**, both
before and after enabling verified TLS. The installed wheel passed all
17 Software TLS checks, including API-verified persistence and independent
retained reader credentials. Logs, JUnit and the installed-wheel JSON are in
[the evidence directory](evidence/).

Full TLS validation exposed two raw Redis test probes that did not receive the
fixture CA even though the native driver did. They now share explicit CA trust
and RESP3 options; verification remains enabled.
[Before](evidence/software-raw-ca-before.log),
[passing full TLS suite](evidence/software-native-tls-full.log).

Cloud Pro installed wheel: all 17 verified-TLS checks passed, including the
retained global SQL/minimal ACL/replay subset. All nine temporary Cloud ACL
resources (three users, roles and rules) were deleted and absence verified.
Full configured Cloud native validation passed: **380 passed, 8 feature skips,
14 required partition cases passed**. The targeted Go source-layout/global SQL
suite also passed, including byte keys, large row IDs, adopted/application/foreign
HASHes, legacy metadata, filters and exact global-result equivalence. The final
foreign-sampling regression also passed on Cloud. The database and
subscription have been deleted and both return HTTP 404; all nine ACL resources
are absent, and local credentials/private TLS material have been removed.
[Ownership, quoted cost and cleanup](evidence/cloud-ownership-and-cleanup.json).

A hosted Linux x86_64 live-Search check reported 1,098/1,099 rows after migrating
one slot, despite cached-ID membership probes succeeding.
[Failure artifact](evidence/ci-post-migration-before.json). The old readiness
probe loaded only SORTABLE IDs. A regression model proves that cached IDs can
pass while whole-HASH loads remain incomplete; it fails before and passes after
requiring the native LOAD @__key + LOAD * shape. The fixture now requires exact
full-load membership on every primary for three consecutive rounds, both before
planning and after migration. Native read/write retries were not added, and
exact Arrow assertions remain unchanged. All **eight consecutive local routing
runs passed** with the stronger gate;
[reports](evidence/routing-hash-readiness/). The documented live Search convergence
limitation remains; committed retained replay is independent of Search indexing.

Both hosted OSS single-node architectures exposed a Redis 8.6 behavior: aggregate
`LOAD @__key` returns NULL attributes for a key containing NUL, although Search's
NOCONTENT document ID preserves every byte. The same failure was reproduced on
an owned local Redis 8.6.2 fixture before changing code. Foreign schema inference
now samples at most 128 document IDs with `FT.SEARCH NOCONTENT` and fetches their
HASH fields; it does not broaden discovery or change data ownership. Strict RESP2
and RESP3 reply tests cover binary identity, owned bytes, empty pages, bounded
pages and malformed responses. The original adopted/application/foreign runtime
regression and existing foreign tests pass after the change. This final change
affects foreign schema sampling; the managed-table benchmark results retain the
source revision and library hash recorded with their runs.

PR #430 merged at `1539e7c`, closing #359/#360/#362 while #363 remained open.
The final corrections continue from main `0297227`. Release CI now also triggers
for partition runtime/checker and foreign-key discovery changes, so these paths
retain packaged-client coverage after the initial workflow PR is merged.

Final follow-up review: [review record](review.md).

The final packaged-client workflow passed on `ff4963a`:
[Release binaries run 39717747137](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39717747137).
All three native build jobs, ten distribution/architecture jobs and twelve
wheel/client cells passed. The release upload job is intentionally skipped on
pull requests. Twelve cells cover Linux x86_64/ARM64 and macOS ARM64, Python
3.13/3.14 and ADBC manager 1.8/1.12, each with 16 verified-TLS checks including
retained minimal-ACL global SQL and replay. Parent jobs also verify Software
proxy TLS and three-primary ASK/MOVED routing. Twenty complete TLS reports,
seven full native JUnit suites (380 passed / 8 feature skips each), and the
14-case minimum-client macOS partition suite are retained in
[packaging evidence](evidence/hosted/release/summary.json). JUnit reports are
compressed without changing their contents; summaries include original SHA256s.
[Workflow/job provenance](evidence/hosted/release/run.json) records the source
revision, job outcomes and successful validation/cleanup steps. This check builds
and tests packages; it does not publish release assets.

All eight final Linux compatibility cells passed on `ff4963a`:
[Test run 39717747124](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39717747124).
Both x86_64 and ARM64 cover OSS single/three-primary Cluster API and Software
single/three-shard proxy deployments. Each full native suite has **380 passed,
8 feature skips and all 14 required partition cases passed**. Go suites,
analytics integrations, required management-helper checks and the applicable
verified-TLS/independent-credential/routing checks passed. Reports and compressed
JUnit are in [compatibility evidence](evidence/hosted/test/summary.json), with
[workflow/job provenance](evidence/hosted/test/run.json).

## Acceptance mapping

| #363 requirement | Final evidence |
|---|---|
| Full configured ADBC and required partitions | Eight compatibility suites; seven release suites; Cloud and Software full TLS suites; explicit feature skips |
| Linux x86_64/ARM64, macOS ARM64 and client versions | Three builds, ten distribution cells, twelve Python 3.13/3.14 × ADBC 1.8/1.12 wheel cells |
| Software/Cloud, TLS, independent workers and routing | Software single/three-shard proxy; owned Cloud Pro one-shard proxy; OSS three-primary TLS with persistent ASK/MOVED workers |
| Malformed/stale descriptors, denied access, cancel/release and cross-process replay | Required native partition gate and expanded retained TLS checker in compatibility/package CI |
| Scoped performance across widths/layouts/counts/concurrency | 168 exact synthetic reads and 90 exact reduced ClickBench reads; recorded versions, hashes, memory, network and full-fetch oracles |
| Reproducibility, paid ownership/cost and cleanup | Per-run artifacts/metadata, commands, Cloud quote and confirmed deletion; local identity-checked cleanup |

Runtime extensions merged in #430 (`1539e7c`); the final foreign-key/readiness/CI
corrections merged in #433 (`abcdb7d`). This record does not claim Cloud multi-shard
validation, a production-scale ClickBench run, an atomic source snapshot, a
process-wide memory ceiling or production readiness. Cloud validation covers the
owned one-shard Pro endpoint; Software and OSS provide the multi-shard checks.
All fixtures created for these final runs have been removed.
