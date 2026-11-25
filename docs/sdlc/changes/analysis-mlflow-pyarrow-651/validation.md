# Validation

2026-11-24, macOS arm64, source `ea16062c` plus this change. Status: local checks
passed; hosted CI and human review pending.

## Fixture

`software-setup.sh` with a dedicated Compose project and ports created Redis
Software 8.2.6-58, database Redis 8.8.0, three shards, Search, TLS and password
authentication. Its gate and a separate management API query both reported
active QPF with scaling factor 4, six Search workers, `mnp` routing and 32
connections. Docker Desktop restarted once during the work and stopped every
container. Setup was rerun and the same gate passed before any recorded result.
The driver came from `make -C go build`.

## Before the change

- Main's `analysis-tests` resolved MLflow 3.18.0, pandas 3.1.1, PyArrow 25.0.1,
  Ibis 12.0.0 and DuckDB 2.0.1 on Python 3.14.8. With the fixture's `REDIS_URI`
  and CA, `pytest python/tests/test_analysis_candidates.py` gave 3 failed and
  2 passed. `test_mlflow_default_empty_digest` reported XPASS(strict).
  Both `test_mlflow_dataset_tracking` cases failed `check_metadata=True`, because
  each result field carries `ARROW:FLIGHT:SQL:TYPE_NAME` (`BIGINT`, `VARCHAR`,
  `DOUBLE PRECISION`, `BOOLEAN`). The data matched with metadata ignored.
- The runner's `REDIS_URI=redis://127.0.0.1:<port>/0` made pytest exit 4:
  "REDIS_URI must use rediss:// with a username and password on Redis Enterprise".
- The strengthened runner contract failed against the old script, receiving
  `redis://127.0.0.1:26379/0` instead of the fixture's `rediss://` URI.

## After the change

| Check | Result |
|---|---|
| `uv pip compile` of the new extra, Python 3.14 | PyArrow 26.0.1, pandas 3.1.1, MLflow 3.18.0, Ibis 12.0.0, DuckDB 2.0.1 |
| `CANDIDATE_PROFILES=analysis python/scripts/run-all-native-candidates.sh`, final extras | 5 passed, 0 failed, 0 errors, 0 skipped, 0 xfailed; runner exit 0 |
| `check_integration_guide.py mlflow --require-read-write` | Read and write passed |
| `check_integration_guide.py ibis --require-read-write` | Read and write passed |
| `python3 -m unittest scripts/test_integration_registry.py` | 11 tests OK |
| `prepare_docs.py`, `mkdocs build --strict`, `check_docs.py` | 45 pages, links and publication scope verified |

The analysis run set `CANDIDATE_COMPOSE_PROJECT`, `SOFTWARE_API_PORT` and
`SOFTWARE_DB_PORT` to the dedicated fixture, and `CANDIDATE_ENV_ROOT` to a
temporary directory. Its runner reran setup and the 4x QPF gate before the tests.
Warnings were MLflow's integer-schema hint and an SQLAlchemy `noload` deprecation
inside MLflow's tracking store.

## Not covered

Other candidate profiles were not run. They use the same changed runner line, but
this record is no evidence for their dependency sets. CI does not install
`analysis-tests`, so hosted CI does not exercise these tests. No Go code changed,
and no Go, ADBC validation or ClickBench run was made.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
