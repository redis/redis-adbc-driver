# Additional direct ADBC candidate validation

- Date: 2026-10-30
- PR: [#258](https://github.com/redis/redis-adbc-driver/pull/258)
- Final main base: `95644965357dec801b7a3b69a31908c7536330ac`.
- Tested source: `d77d81b7180a86ff6230187b7a4c8c1bc20091a1` (this follow-up plus
  the final main base). The runner built or reused the native library from the
  matching Go source tree.
- Driver implementation unchanged by this PR. Earlier candidate runner: #248.
- Scope: native Redis ADBC only. No Flight SQL path was tested.
- [Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md)

## Five additional entries

All five custom workflows passed their populated and empty-result checks in
focused runs against Redis Software 8.6.2, Search, one shard, with active verified
4x QPF. The separate MLflow default-empty-digest regression remains an exact,
strict expected failure; the empty workflow uses its public explicit-digest API.
The unchanged complete fourteen-profile launcher exited **0**: **152 passed,
one skipped, seven strict expected-failure cases, zero failures/errors**. The
skip is the ordinary plaintext Ray group's TLS-specific case; the dedicated
private-CA profile passed all 17 cases with no skips. The expected failures are
four adbi 0.1.3 binding variants, two Datasets 5.1.0 default empty saves, and one
MLflow automatic empty digest. Every profile's launcher exited 0.

[Machine-readable complete results](native-results.json) ·
[Profile versions](environment-versions.json).

| Profile | Passed | Skipped | Strict expected failures |
|---|---:|---:|---:|
| core | 97 | 1 | 0 |
| workflows | 6 | 0 | 2 |
| models | 7 | 0 | 0 |
| orchestrators | 3 | 0 | 0 |
| Spark | 1 | 0 | 0 |
| Airflow | 7 | 0 | 0 |
| Superset | 1 | 0 | 0 |
| dbt | 1 | 0 | 0 |
| documents | 2 | 0 | 4 |
| analysis: Ibis/MLflow | 4 | 0 | 1 |
| Beam | 2 | 0 | 0 |
| Mage | 2 | 0 | 0 |
| Kestra | 2 | 0 | 0 |
| Ray TLS | 17 | 0 | 0 |

The final run used macOS arm64. All four new profiles used Python 3.12.16,
ADBC driver manager 1.13.0, and pytest 9.2.0:

| Profile | Application/runtime versions | PyArrow | pandas |
|---|---|---|---|
| analysis | Ibis 12.0.0, DuckDB 2.0.0, MLflow 3.17.2 | 25.0.1 | 3.1.0 |
| Beam | apache-beam 2.77.0, NumPy 2.4.6 | 25.0.1 | Not installed |
| Mage | mage-ai 0.9.80 | 25.0.1 | 2.3.3 |
| Kestra | server 2.0.7, Python Script plugin 1.18.6, Python SDK 2.0.0, Temurin 25.0.5+8 | 26.0.0 | Not installed |

The earlier nine profiles retain their separate dependency stacks documented in
the [previous sweep](../candidate-test-runner/native-sweep.md#tested-environments).
The final command also incorporates PR #256's expanded R/Datasets cases and Ray
TLS profile. That profile uses Python 3.12.16, Ray 2.59.0, PyArrow 26.0.0, and a
fresh owned Software instance; its container, network, and private CA material
were removed afterward. Six separate TLS launcher ownership/cleanup/reporting
unit checks passed. They are not added to the pytest totals above.
The complete command supplied Java 21, R, and Quarto for the earlier groups.
Each group invoked the repository's Software/QPF verification before testing.
Runtime logs/JUnit/exit-code artifacts remain outside the repository; only
sanitized counts and package versions are committed. The earlier 124-pass record
is retained separately and was not relabeled as this run. The earlier
thirteen-profile run on `6a31164` reported 134 passed/one skip/two expected
failures before #256 added its cases; it is not the source of the final totals.

| Entry | Checks and scope |
|---|---|
| Ibis | Native bound SQL fetch → Arrow → `memtable` with a local DuckDB backend. Exact rows/schema, NULLs/Unicode, filters, counts, non-null counts, sums, and empty aggregates. This is a fetched-data composition, not a Redis Ibis backend or Redis pushdown. |
| MLflow | Native bound SQL fetch → Arrow/pandas → local SQLite tracking. Verify dataframe values, dataset digest/schema/profile, finished run, recorded dataset input/context, metrics, and downloaded JSON artifact. No Redis MLflow backend is supplied. |
| Apache Beam | A custom Python DoFn opens/closes the native connection inside a separate local FnApi worker process. Verify its PID differs from the pipeline process, and check full rows/Arrow schema for populated and empty reads. Remote runners, streaming sources/sinks, and a packaged I/O connector are untested. |
| Mage | Execute `mage init` and `mage run` for a real local loader/transformer pipeline. The loader performs native bound queries; the downstream block checks persisted dataframe rows/dtypes, NULLs/Unicode, and empty columns. Browser/scheduler/hosted flows are untested. |
| Kestra | Start an owned local 2.0.7 server with temporary H2/storage, loopback binding, generated local authentication, and the official Python Script plugin. Read through native ADBC, hand off a JSON file through Kestra storage, check rows/schema in a second Python process, and retrieve/verify the task output through the current API. Stop the server after the run. |

## Compatibility findings and upstream tracking

- MLflow 3.17.2 requires `pyarrow>=4,<26`; Beam 2.77.0 requires
  `pyarrow>=14.0.1,<26`. Their profiles use PyArrow 25.0.1. The public package's
  `dbapi` extra requires PyArrow 26; these isolated test extras install the base
  Python package plus the compatible application stack, and validate that native
  path explicitly. Upstream caps were not overridden.
- The user's PyArrow-upgrade request is tracked by existing
  [MLflow #27201](https://github.com/mlflow/mlflow/issues/27201), whose fix
  [#27216](https://github.com/mlflow/mlflow/pull/27216) merged October 28, and newly
  opened [Beam #40919](https://github.com/apache/beam/issues/40919). PyPI's latest
  PyArrow was 26.0.0 when checked. MLflow's installed release still has the old cap.
- [MLflow #27358](https://github.com/mlflow/mlflow/issues/27358): automatic digest
  calculation on a pandas 3.1.0 empty Arrow string column raises
  `NotImplementedError: eq not implemented for <class 'type'>`. Reproduced with a
  locally constructed Arrow table, without Redis or ADBC. For the empty workflow,
  the harness derives an explicit digest from the Arrow IPC content/schema. A
  separate strict expected failure retains the default path, scoped to MLflow
  3.17.2, pandas 3.1.0, the exception type, and exact message. Other failures fail
  normally; a successful default path triggers a strict unexpected pass.
- Kestra 2.0.7 requires Java 25 (class version 69), despite the standalone docs'
  Java 21 wording. A separate checksum-verified Temurin 25 runtime was used; Spark
  continues to use Java 21. The saved preparation script verifies Kestra's pinned
  executable checksum and installs Python Script plugin 1.18.6.
- Existing upstream warnings remain visible, including MLflow SQLAlchemy/pandas
  deprecations and integer-schema guidance, and Mage dependency deprecations.

## Reproduction

See the [Python README](../../../../python/README.md#rerun-candidate-integration-tests)
for all prerequisites and individual tool selectors. To run just these five tools
in compatible isolated environments:

```sh
CANDIDATE_PROFILES="analysis beam mage kestra" KESTRA_JAVA=/path/to/java25/bin/java \
  python/scripts/run-all-native-candidates.sh
```

Omit `CANDIDATE_PROFILES` for the complete set. Each selected profile writes a
log, JUnit XML, and launcher exit code; the summary includes only the selected
profiles and preserves failed, skipped, and expected-failure outcomes.

## Matrix coverage

These five add entries 13, 27, 36, 37, and 38 to the previous sweep's 32 entries:
**37 of 50 entries now have exercised native paths**, including the separately
recorded manual RStudio check. This is not a claim that every operation or hosted
deployment is supported. The previous [50-row record](../candidate-test-runner/native-sweep.md)
remains historical evidence for its earlier scope.

The 13 remaining entries still need connector/adapter implementation or have
only an excluded Flight proposal in this comparison: Kafka/Connect, Flink,
Trino, NiFi, Airbyte, Tableau, Metabase, Great Expectations, Soda, Power BI,
Meltano, Apache Hop, and Dataform. Custom integrations may be possible; they are
not counted as passing tests here.
