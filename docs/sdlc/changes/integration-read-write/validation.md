# Read/write integration verification

Date: 2026-11-01. Public source: `68b44d0` plus this change. Author: Codex.
Status: local verification passed; human review and hosted CI pending.
[Intent](intent.md), [spec](spec.md), [plan](plan.md),
[per-guide results](results.json), [dependency versions](versions.json).

## Environment and scope

macOS ARM64. The native library was rebuilt from current public main with
`GOCACHE=<writable-cache> make -C go build`, preserving the Makefile version flag.
A dedicated disposable Redis Software fixture was created with
`COMPOSE_PROJECT_NAME=<owned-project> SOFTWARE_API_PORT=<local-port> SOFTWARE_DB_PORT=<local-port> make -C go redis-up`.
The setup verified Redis 8.6.2, Search, one shard and **active 4x QPF** before
these runs. It used a non-default data endpoint, deliberately exercising URI
configuration rather than relying on localhost:6379.

Each guide ran in its preinstalled isolated dependency-compatible environment.
Most use Python 3.12.16; Airflow and dbt use Python 3.14.10. The version report
records the exact Python/library versions, including manager 1.13.0, Arrow 26,
pandas 3.1.0, Polars 2.1.0, Ray 2.59.0 and DuckDB 2.0.0. Analysis and Beam retain
their separately compatible Arrow environments. Kestra used the guide's pinned
2.0.7 runtime/Python Script plugin; R and Quarto used the already installed local
runtimes and configured library paths. No new hosted-platform validation occurred.

## Executed checks

With `REDIS_URI`, `REDIS_ADBC_LIBRARY`, `PYTHONPATH`, and the runtimes from each
guide configured:

```sh
python python/scripts/run-integration-guides.py \
  --env-root /path/to/isolated-environments --results /tmp/read-write-results \
  --tools pandas polars sqlalchemy pyarrow numpy scikit-learn dagster dask \
  prefect dlt ray duckdb jupyter huggingface-datasets streamlit scipy pytorch \
  tensorflow keras xgboost transformers plotly matplotlib spark kubeflow \
  metaflow ibis mlflow beam mage kestra r-dbi rstudio quarto airflow dbt
python -m pytest -q python/tests/test_guide_output.py python/tests/test_guide_runner.py
```

- **36/36 actual guides passed with required read/write evidence.** This covers
  all **34 green local-ADBC rows** in the 50-tool table, plus Spark's custom bridge
  and DuckDB. Superset and Fivetran's source SDK were not counted or rerun.
- **6 focused tests passed**, including populated/empty output, quoted column
  names, duplicate/order preservation, NULLs/Unicode, independent-reader failure
  cleanup, rejection of missing evidence, and rejection of a failed process even
  if its output contains the success marker.
- `git diff --check`, Python compilation and parsing of all Python guide blocks
  passed. Core guide checks were added to CI; no hosted CI outcome is claimed.

The first sweep found a reserved helper column name, an import-shadowing error
in the new Mage snippet, and an existing hardcoded Ray endpoint. Those were
corrected. The seven affected guides were rerun and passed; `results.json`
identifies the recheck entries rather than presenting earlier failures as passes.
The other guides passed after the common helper correction. The final sink tests
were rerun after making their cleanup assertion specific to the owned table.

## What the writes establish

The native DataFrame, SQLAlchemy, Airflow, Dagster, dlt, Ray SQL, dbt, DuckDB and
R paths execute real writes and validate fetched output. The other examples
persist tool-computed predictions, transformed rows, chart data or task summaries
through an explicit Arrow helper; they do not count input fixture setup as the
write integration. The helper fully fetches results on a fresh connection,
compares Arrow values/types and original row order, and removes its owned table.
Dask writes the computed materialized result on the caller. Beam writes its
materialized worker output from the caller; Mage, Kubeflow, Metaflow and Kestra
write their outputs inside the documented local worker/task process. RStudio's guide was checked through Rscript,
without claiming a new IDE/UI run. Quarto rendered its actual document.

## Issues and limitations

- [#274](https://github.com/redis/redis-adbc-driver/issues/274): executable guide
  blocks ignored `REDIS_URI`; fixed here, including the linked DuckDB SQL harness.
- [#273](https://github.com/redis/redis-adbc-driver/issues/273): Dask still lacks a
  packaged worker-local Arrow sink for outputs larger than caller memory.
- [#272](https://github.com/redis/redis-adbc-driver/issues/272): the generic Airflow
  provider/operator path retains transaction/discovery defaults incompatible
  with this driver. The existing packaged Redis hook works and was validated;
  this issue tracks upstream/provider selection work, not a missing hook feature.

All writes autocommit; partial failure, retry duplicates, and abrupt-process
cleanup limits remain. No driver transaction or consistency feature changed.
The output examples do not add model/artifact stores, native ML/plotting database
connectors, CDC, remote-cluster guarantees, Flight SQL writes, or release-wheel
validation. Existing strict expected failures and compatibility limits in other
suites were not broadened. The native Go suite was not rerun because this change
does not alter native code. The older-checkout exploratory checks made before
fetching current public main are excluded from these results.

## Application-integration guidance follow-up

The README, Python package README, setup guide, Python integration overview,
guide index and ecosystem comparison now identify official Redis client
libraries as the standard application-integration path. Redis ADBC is presented
as another option for SQL/Arrow workflows. The link to Redis's client-library
documentation was verified on 2026-11-01. This follow-up changes prose only;
`git diff --check` passed and no runtime tests were needed.
