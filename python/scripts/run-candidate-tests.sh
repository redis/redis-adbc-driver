#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python_bin="${PYTHON:-python3}"

usage() {
  echo "Usage: $0 [all | TOOL ...]"
  echo "Tools: driver airflow dagster dask duckdb pandas polars ml ray prefect dlt jupyter datasets streamlit scipy"
}
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
  usage
  exit 0
fi
if [[ $# == 0 || ( $# == 1 && "${1:-}" == all ) ]]; then
  set -- driver airflow dagster dask duckdb pandas polars ml ray prefect dlt jupyter datasets streamlit scipy
fi
tests=()
for tool in "$@"; do
  case "$tool" in
    driver) tests+=(test_driver.py) ;;
    airflow) tests+=(test_airflow_hook.py test_airflow.py) ;;
    dagster) tests+=(test_dagster.py test_dagster_adbc.py) ;;
    dask) tests+=(test_dask.py) ;;
    duckdb) tests+=(test_duckdb.py) ;;
    pandas) tests+=(test_pandas.py test_pandas_direct_reads.py test_pandas_writes.py) ;;
    polars) tests+=(test_polars.py) ;;
    ml) tests+=(test_ml_ingest.py) ;;
    ray) tests+=(test_ray.py test_ray_arrow.py test_ray_datasource.py) ;;
    prefect) tests+=(test_prefect.py) ;;
    dlt) tests+=(test_dlt.py) ;;
    jupyter) tests+=(test_jupyter.py) ;;
    datasets) tests+=(test_hf_datasets.py) ;;
    streamlit) tests+=(test_streamlit.py) ;;
    scipy) tests+=(test_scipy.py) ;;
    *) usage >&2; echo "Unknown tool: $tool" >&2; exit 2 ;;
  esac
done
paths=()
for test_file in "${tests[@]}"; do
  paths+=("$repo_root/python/tests/$test_file")
done

export COMPOSE_PROJECT_NAME="${CANDIDATE_COMPOSE_PROJECT:-redis-adbc-candidates}"
export SOFTWARE_API_PORT="${SOFTWARE_API_PORT:-29443}"
export SOFTWARE_DB_PORT="${SOFTWARE_DB_PORT:-26379}"
# The repository setup verifies active 4x QPF and effective Search routing on
# both new and reused Redis Software databases before tests start.
make -C "$repo_root/go" redis-up

make -C "$repo_root/go" build
case "$(uname -s)" in
  Darwin) driver_extension=dylib ;;
  Linux) driver_extension=so ;;
  *) echo "unsupported driver build platform: $(uname -s)" >&2; exit 1 ;;
esac
export REDIS_URI="redis://127.0.0.1:${SOFTWARE_DB_PORT}/0"
export REDIS_ADBC_LIBRARY="$repo_root/go/build/libadbc_driver_redis.${driver_extension}"
export PYTHONPATH="$repo_root/python${PYTHONPATH:+:$PYTHONPATH}"
export RUN_DUCKDB_TESTS=1
export RUN_AIRFLOW_TESTS=1
export RUN_RAY_TESTS=1
export PREFECT_SERVER_ANALYTICS_ENABLED=false
export PREFECT_LOGGING_LEVEL=ERROR

"$python_bin" -m pytest -q -rs "${paths[@]}"
