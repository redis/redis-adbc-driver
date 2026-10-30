#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python_bin="${PYTHON:-python3}"
container_name="${REDIS_TEST_CONTAINER:-redis-adbc-python-test}"
redis_port="${REDIS_TEST_PORT:-16379}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to start the Redis test instance" >&2
  exit 1
fi

if ! docker inspect "$container_name" >/dev/null 2>&1; then
  docker run --detach --name "$container_name" \
    --publish "127.0.0.1:${redis_port}:6379" redis:8.6.2
else
  docker start "$container_name" >/dev/null
fi

ready=0
for _ in $(seq 1 30); do
  if docker exec "$container_name" redis-cli ping 2>/dev/null | rg -q '^PONG$'; then
    ready=1
    break
  fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then
  echo "Redis container $container_name did not become ready" >&2
  exit 1
fi
docker exec "$container_name" redis-cli FT._LIST >/dev/null

make -C "$repo_root/go" build
case "$(uname -s)" in
  Darwin) driver_extension=dylib ;;
  Linux) driver_extension=so ;;
  *) echo "unsupported driver build platform: $(uname -s)" >&2; exit 1 ;;
esac
export REDIS_URI="${REDIS_URI:-redis://127.0.0.1:${redis_port}/0}"
export REDIS_ADBC_LIBRARY="$repo_root/go/build/libadbc_driver_redis.${driver_extension}"
export PYTHONPATH="$repo_root/python${PYTHONPATH:+:$PYTHONPATH}"
export RUN_DUCKDB_TESTS=1
export RUN_AIRFLOW_TESTS=1
export RUN_RAY_TESTS=1
export PREFECT_SERVER_ANALYTICS_ENABLED=false
export PREFECT_LOGGING_LEVEL=ERROR

"$python_bin" -m pytest -q \
  "$repo_root/python/tests/test_driver.py" \
  "$repo_root/python/tests/test_airflow_hook.py" \
  "$repo_root/python/tests/test_airflow.py" \
  "$repo_root/python/tests/test_dagster.py" \
  "$repo_root/python/tests/test_dagster_adbc.py" \
  "$repo_root/python/tests/test_dask.py" \
  "$repo_root/python/tests/test_duckdb.py" \
  "$repo_root/python/tests/test_pandas.py" \
  "$repo_root/python/tests/test_pandas_direct_reads.py" \
  "$repo_root/python/tests/test_pandas_writes.py" \
  "$repo_root/python/tests/test_polars.py" \
  "$repo_root/python/tests/test_ml_ingest.py" \
  "$repo_root/python/tests/test_ray.py" \
  "$repo_root/python/tests/test_ray_arrow.py" \
  "$repo_root/python/tests/test_ray_datasource.py" \
  "$repo_root/python/tests/test_prefect.py" \
  "$repo_root/python/tests/test_dlt.py"
