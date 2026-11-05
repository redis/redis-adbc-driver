# Live integration guide validation

Date: October 30, 2026. Author/reviewer: OpenAI Codex; agent verification is not
human approval. Requested by the user after the guides were written.
Issue: [#249](https://github.com/redis/redis-adbc-driver/issues/249).
PR: [#251](https://github.com/redis/redis-adbc-driver/pull/251).
[Intent](intent.md), [specification](spec.md), [plan](plan.md),
[additional guide scope](new-workflows.md).

## Sources and environment

Native driver and Python adapter source:
`a99c882b4088aaff32e1abdad449f6e1b575b41a` (#248). The native library was
built with `make -C go build`, Go 1.27.2, on macOS arm64. Its SHA-256 was
`2030025127fb49abc30ec90e460d5e6d07b69c17e8c5c5f93bd72e5f4a1f1d8c`.
The six new guide examples came from #251's
`dc88ac6b16ac8b2ba22bfc8646f6dbdd3039f9d3`; this follow-up adds the tested
dbt starter guide and corrected DuckDB SQL.

A dedicated, disposable **Redis Software** fixture used the repository's pinned
`redislabs/redis:8.2.0-78.18` image, Redis **8.6.2**, one RAM shard and Search.
`make -C go redis-up` verified **active QPF scaling factor 4**, **6 Search
workers**, **mnp routing** and **32 connections** before the examples ran.
No existing application Redis database was used. Fixture identifiers, endpoints,
credentials and raw operational logs are intentionally not published.

Python environments were kept separate:

| Environment | Python | Relevant packages |
|---|---|---|
| General examples | 3.12.16 | ADBC manager 1.13.0, Arrow 26.0.0, NumPy 2.5.4, scikit-learn 1.9.1, pandas 3.1.0, Polars 2.1.0, SQLAlchemy 2.1.6, Dask/Distributed 2026.8.0, Prefect 3.9.1, dlt 1.32.0, DuckDB 2.0.0 |
| New notebook/app/data workflows | 3.15.2 | Arrow 26.0.0, SciPy 1.18.1, Datasets 5.1.0, Streamlit 1.66.1, ipykernel 7.4.0, nbclient 0.11.0, nbformat 5.11.1 |
| Dagster | 3.12.14 | Dagster 1.13.27, dagster-adbc 0.0.2, Arrow 26.0.0 |
| Ray | 3.12.14 | Ray 2.59.0, Arrow 26.0.0 |
| Airflow | 3.14.10 | Airflow 3.3.2, Apache Arrow provider 0.1.1, Arrow 26.0.0; NumPy 2.5.4 supplied in a task-local dependency directory |
| Superset | 3.12.16 | Superset 6.1.0, SQLAlchemy 1.4.54, pandas 2.1.4, Arrow 18.1.0 |
| dbt | 3.14.10 | dbt Core 1.12.6, dbt-redis-adbc 0.1.0, Arrow 26.0.0 |

The dbt adapter checkout was
`c9574b98762f4ff80b60a92b2d4137c2b03bf9df`.
DuckDB installed and loaded the signed Query.Farm scanner and the signed
community `redis_adbc` extension for DuckDB 2.0.0. The standalone extension
uses its embedded driver; it is not the separately built library above.

## Results: all 18 indexed integration paths passed

These are fresh local example/workflow results, not a claim that every
integration's entire regression suite passed.

| Guide | Execution and independently checked outcome |
|---|---|
| NumPy | Exact read-only SQL/Arrow-to-array example; asserted values [1,2,3] and printed sum 6. |
| scikit-learn | Exact feature/label query and decision-tree example; predicted labels equal expected labels. |
| Jupyter | Exact Python cell executed in a separate kernel; its assertion checked answer 42 and Unicode. Kernel execution passed through the saved checker after its kernel-discovery fix. |
| Hugging Face Datasets | Exact guide: unique-table ingest/fetch, Arrow conversion, local save/reload with one shard, feature equality and filtered rows. |
| Streamlit | Exact guide app through AppTest; initial rows [1,2,3]/metric 3 and input rerun rows [2,3]/metric 2; no app exceptions. |
| SciPy | Exact unique-table sparse-coefficient example; asserted solution [2,3] and residual through matrix multiplication. |
| pandas | Both fenced examples executed: direct SQL read and unique-table write/read round trip with DataFrame equality. |
| Polars | Both fenced examples executed: URI read and unique-table DataFrame write/read equality. |
| SQLAlchemy | Exact engine/reflection/bound-query block, substituting only the dedicated fixture URI; schema inspection and fetched parameter value 42 succeeded. |
| Prefect | Exact flow/task block, with task-local Redis connection; returned/fetched Arrow row was {'one':1}. |
| Dask | Inline partitioned query over prepared orders fixture, checked amount sum 100; linked dask_read.py also passed with real worker processes and asserted partition sizes, rows and dtypes. |
| Dagster | Both fenced definitions actually materialized: ADBCResource order_count was 2, IO manager persisted rows [(1,10),(2,20)]. Linked dagster_smoke.py also passed its dependent assets and SQL assertion (3,42). |
| dlt | Inline pipeline and linked dlt_pipeline.py both ran; a clean-table rerun independently asserted exact persisted amounts [(1,10.5),(2,20.0)]. Pipeline state directories were isolated. |
| Ray Data | Both fenced blocks ran with local Ray workers. The worker-side reader fully consumed its result and asserted IDs [100,101]; SQL reader fetched {'one':1}. |
| Airflow | Exact RedisAdbcHook block returned [(42,)]. The guide-linked test_airflow.py passed **2 tests, zero skips**, including the full DAG twice and official Arrow hook operations. |
| Superset | The described engine/dataset/SQL Lab workflow passed test_superset.py: **1 test, zero skips**, isolated temporary metadata, schema/table discovery, dataset metadata, grouped/time-series queries and raw SQL Lab execution. |
| DuckDB | Both fenced SQL blocks executed statement-by-statement with results fetched, including signed scanner installation, read-only attachment, schema USE, remote aggregation, ingest and local join. Persisted rows were checked through ADBC. The linked standalone redis_adbc_scan example also passed using the signed community extension. |
| dbt Core v1 | The new local starter guide's debug, seed, build (including singular test), docs generate and show commands passed. ADBC independently checked persisted (order_count,total)=(3,42). Its Python cleanup example ran and schema absence was verified. |

Read examples with named tables received isolated prerequisite fixtures matching
their documented schemas. Hardcoded local URIs were redirected to the dedicated
fixture; source-library/manifest paths were supplied through the documented
environment variables. Dagster definitions were materialized rather than merely
imported, and Ray reads were consumed rather than only planned.

## Failure found and corrected

The original DuckDB guide sets `USE r.public` and then tries `DETACH r`.
On DuckDB 2.0.0 this fails with:

```text
Cannot detach database "r" because it is the default database.
```

The guide now switches to `USE memory` before detaching. Both complete SQL
blocks and the written rows passed after this correction.

Validation setup also needed two fixes: temporary example filenames initially
shadowed third-party modules, and the existing Airflow environment lacked NumPy.
The examples were rerun with safe filenames and a task-local NumPy install.
The saved Jupyter checker initially supplied kernel-search paths only to the
child; setting them for parent-side kernel discovery fixed it, and the same
guide cell then passed. These were harness/environment failures, not softened
example assertions. Earlier attempts are not counted as successful runs.

## Reproduction and limits

For the six new guides, the saved checker executes each guide's actual fenced
Python block. Install the selected tool and driver in the active environment,
set `REDIS_URI` to a dedicated database with Search, and run:

```sh
python python/scripts/check_integration_guide.py numpy
python python/scripts/check_integration_guide.py scikit-learn
python python/scripts/check_integration_guide.py jupyter
python python/scripts/check_integration_guide.py huggingface-datasets
python python/scripts/check_integration_guide.py streamlit
python python/scripts/check_integration_guide.py scipy
```

Use separate compatible interpreters where required. For source builds, set
`REDIS_ADBC_LIBRARY` and `PYTHONPATH=python` before launching subprocesses or
kernels. The checker was itself exercised against Redis for all six selections.
The other guide blocks and linked workflow scripts/tests listed above were
executed with their source-checkout commands and isolated prerequisite fixtures.

This run does not validate installation into clean environments, all published
wheels/platforms, TLS, Redis Cluster, hosted services or multi-node orchestration.
JupyterLab/Streamlit browser UI and rendered Superset charts were not tested;
kernel execution, AppTest and the Superset server workflow were tested.
The larger external NYC taxi project and experimental dbt v2 engine were not
rerun. There was no full documentation-site build, performance benchmark or
claim of upstream acceptance. The dedicated Redis fixture was removed after
validation.
