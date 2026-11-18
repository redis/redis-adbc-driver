#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python_bin="${PYTHON:-python3}"

usage() {
  echo "Usage: $0 [all | acceptance [SUITE ...] | TOOL ...]"
  echo "Tools: $(python3 "$repo_root/python/scripts/integration_registry.py" tools)"
}
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
  usage
  exit 0
fi
if [[ "${1:-}" == acceptance ]]; then
  shift
  exec "$python_bin" "$repo_root/python/scripts/run-acceptance.py" "$@"
fi
if [[ $# == 0 || ( $# == 1 && "${1:-}" == all ) ]]; then
  exec "$repo_root/python/scripts/run-all-native-candidates.sh"
fi
test_names="$(python3 "$repo_root/python/scripts/integration_registry.py" tests "$@")"
tests=()
while IFS= read -r test_file; do tests+=("$test_file"); done <<< "$test_names"

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

pytest_args=(-q -ra)
if [[ -n "${CANDIDATE_JUNIT_XML:-}" ]]; then
  pytest_args+=("--junitxml=$CANDIDATE_JUNIT_XML")
fi
exec "$python_bin" -m pytest "${pytest_args[@]}" "${paths[@]}"
