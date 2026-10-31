# Integration guide validation

Date: 2026-10-30 (America/Los_Angeles).
Source base: `3fd6bf36eef8f6ab069a6ee06929e972a40c11f3`; the native driver is unchanged by this update.
PR: [#262](https://github.com/redis/redis-adbc-driver/pull/262).
Author: Codex. Status: local verification complete; human review pending.

## Result

**38 of 38 guide checks passed; zero failed, zero skipped.** This is a count of
guide workflows, not individual application test cases. The final checked-in
runner completed successfully and rewrote its report from an empty initial
state. See [machine-readable results and versions](results.json).

The driver was built with `make -C go build`. `make -C go redis-up` reported
Redis Software, Redis 8.6.2, one shard, Search and active verified **4x QPF**.
The platform was macOS arm64. Existing isolated dependency environments were
reused, rather than freshly installing every guide's dependencies. This tests
the guide code with the recorded versions and the current source-built library;
it does not validate released wheel contents or unconstrained future releases.

## Per-guide checks

| Guide | Executed path | Result |
|---|---|---|
| [pandas](../../../integrations/pandas.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Polars](../../../integrations/polars.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [SQLAlchemy](../../../integrations/sqlalchemy.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [PyArrow](../../../integrations/pyarrow.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [NumPy](../../../integrations/numpy.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [scikit-learn](../../../integrations/scikit-learn.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Dagster](../../../integrations/dagster.md) | Linked self-cleaning asset materialization example | Passed |
| [Dask](../../../integrations/dask.md) | Linked example with two worker processes and five query partitions | Passed |
| [Prefect](../../../integrations/prefect.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [dlt](../../../integrations/dlt.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Ray Data](../../../integrations/ray.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [DuckDB](../../../duckdb.md) | Both guide SQL blocks through the signed adbc_scanner extension | Passed |
| [Jupyter](../../../integrations/jupyter.md) | Actual guide block in a separate Python kernel | Passed |
| [Hugging Face Datasets](../../../integrations/huggingface-datasets.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Streamlit](../../../integrations/streamlit.md) | Actual guide app with AppTest and a filter-input rerun | Passed |
| [SciPy](../../../integrations/scipy.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [PyTorch](../../../integrations/pytorch.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [TensorFlow](../../../integrations/tensorflow.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Keras](../../../integrations/keras.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [XGBoost](../../../integrations/xgboost.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Hugging Face Transformers](../../../integrations/transformers.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Plotly](../../../integrations/plotly.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Matplotlib](../../../integrations/matplotlib.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Apache Spark](../../../integrations/spark.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Kubeflow Pipelines](../../../integrations/kubeflow.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Metaflow](../../../integrations/metaflow.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Fivetran Connector SDK](../../../integrations/fivetran.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Ibis](../../../integrations/ibis.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [MLflow](../../../integrations/mlflow.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Apache Beam](../../../integrations/beam.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Mage](../../../integrations/mage.md) | Actual Python guide block(s), including expected-result assertions | Passed |
| [Kestra](../../../integrations/kestra.md) | Linked runnable local server/two-task example and downstream outputs | Passed |
| [R / DBI](../../../integrations/r-dbi.md) | Actual query block through Rscript | Passed |
| [RStudio](../../../integrations/rstudio.md) | Actual query block through Rscript and manually in RStudio console | Passed |
| [Quarto](../../../integrations/quarto.md) | Actual QMD rendered to HTML, with executed output marker checked | Passed |
| [Apache Airflow](../../../integrations/airflow.md) | Actual Redis hook block, plus documented DAG tests (2 passed) | Passed |
| [Apache Superset](../../../integrations/superset.md) | Documented isolated backend smoke (1 passed); browser not exercised | Passed |
| [dbt Core](../../../integrations/dbt.md) | Actual starter files; debug, seed, build, docs generate, show and cleanup | Passed |

## Reproduction

Prepare the compatible environments using the profiles/extras documented in
`python/scripts/run-all-native-candidates.sh` and `python/pyproject.toml`.
Build the native library and verify the Software fixture as above. Set
`REDIS_URI` and `REDIS_ADBC_LIBRARY`, and configure `R_LIBS_USER`, `QUARTO`,
`JAVA_HOME`, `KESTRA_JAVA`, `KESTRA_JAR` and `KESTRA_PLUGINS_PATH` where needed.
The guide runner does not install software or change these environments:

```sh
python python/scripts/run-integration-guides.py \
  --env-root /path/to/compatible-profile-environments \
  --results /path/to/new-guide-results
```

Every guide also provides a single-entry command through
`python/scripts/check_integration_guide.py TOOL`. The checker executes the actual
Markdown blocks or the runnable workflow explicitly linked by that guide.
For existing-table Dask/Dagster sketches, the linked self-contained examples
provide the runnable path and own/clean their fixtures.

Native/document runtimes: R 4.6.2, DBI 1.3.0, adbi 0.1.3,
adbcdrivermanager 0.25.0; Quarto 1.10.20; Spark used JDK 21; Kestra 2.0.7
used Java 25 and the pinned Python Script plugin. RStudio's actual console
execution printed `REDIS_ADBC_R_DBI_OK` and wrote the success report after the
guide's checks and table cleanup. Python/package versions appear in `results.json`.

## Failures found and corrected

- The first Quarto draft used a tilde-delimited executable cell. Quarto rendered
  HTML without executing that cell, and the output-marker assertion failed.
  The guide now uses a backtick executable cell inside a four-backtick outer
  documentation fence. The corrected document executed and passed.
- Initial launcher paths did not point to the installed Java runtime and R
  package library. Correcting those environment paths allowed the unchanged
  Kestra and R example code to pass. No unavailable runtime was counted as a pass.
- The initial dbt checker assumed the `profiles.yml` introduction ended
  immediately after the filename. Its Markdown extraction failed before dbt
  ran. The corrected extractor reads the actual starter files and all five dbt
  commands passed; the persisted `(3, 42)` result was checked independently.

## Limits and self-review

Superset checks its application backend, dataset/chart SQL and SQL Lab, not
browser navigation or hosted deployment. Streamlit uses AppTest; Plotly HTML
serialization is checked without browser interaction. RStudio checks one
console workflow, not the whole IDE. DuckDB checks adbc_scanner; its standalone
redis_adbc extension has separate build/tests. ML models are tiny CPU examples,
and orchestration runs locally. No remote clusters, hosted services, Flight SQL,
GPU training, large-data performance or new TLS validation are claimed here.
Existing R binding and upstream dataset/digest limitations remain documented.

Self-review followed REVIEW.md's lifecycle, ownership and evidence passes.
The shared helper uses unique owned tables and closes connections/cursors;
linked examples retain their cleanup. Fixed-name dbt/DuckDB checks refuse to
overwrite existing objects. No credentials, private infrastructure identifiers,
runtime logs or license artifacts are committed. README coverage, relative
links/anchors, Python-block parsing and `git diff --check` were checked.
No blocking issue was found in this review; this is not human approval.

See [intent](intent.md), [specification](spec.md) and [plan](plan.md).
