#!/usr/bin/env bash
# A fresh, owned Software proxy TLS fixture. Trust stays on the Redis connection.
set -euo pipefail
umask 077
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python_bin="${PYTHON:-python3}"
export COMPOSE_PROJECT_NAME="${CANDIDATE_TLS_COMPOSE_PROJECT:-redis-adbc-ray-tls-$$}"
export SOFTWARE_API_PORT="${CANDIDATE_TLS_API_PORT:-30443}"
export SOFTWARE_DB_PORT="${CANDIDATE_TLS_DB_PORT:-27379}"
export SOFTWARE_SHARDS=1
compose=(docker compose -f "$repo_root/go/compose.yaml")
# Refuse to mutate or clean up any pre-existing project, even an inactive one.
existing="$("${compose[@]}" ps --all --quiet)"
if [[ -n "$existing" ]]; then
  echo "ray-tls requires a fresh Compose project" >&2
  exit 2
fi
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/redis-adbc-ray-tls.XXXXXX")"
cleanup() {
  local result=$?
  trap - EXIT
  if ! "${compose[@]}" down --volumes; then result=1; fi
  rm -rf "$fixture_root"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
make -C "$repo_root/go" redis-up
container="$("${compose[@]}" ps --quiet redis-software)"
"$python_bin" "$repo_root/scripts/software_partition_tls.py" "$fixture_root/tls" \
  --container "$container" --api-port "$SOFTWARE_API_PORT" \
  --db-port "$SOFTWARE_DB_PORT" --shards 1
make -C "$repo_root/go" build
case "$(uname -s)" in
  Darwin) driver_extension=dylib ;;
  Linux) driver_extension=so ;;
  *) echo "unsupported driver build platform" >&2; exit 2 ;;
esac
export REDIS_URI="$(cat "$fixture_root/tls/admin.uri")"
export REDIS_TLS_CA_FILE="$fixture_root/tls/ca.crt"
export REDIS_ADBC_LIBRARY="$repo_root/go/build/libadbc_driver_redis.${driver_extension}"
export PYTHONPATH="$repo_root/python${PYTHONPATH:+:$PYTHONPATH}"
export RUN_RAY_TESTS=1 REQUIRE_RAY_TLS=1 RAY_USAGE_STATS_ENABLED=0
# Do not set SSL_CERT_FILE, REQUESTS_CA_BUNDLE or package-manager trust options.
# Short tracebacks omit fixture argument reprs, including the authenticated URI.
pytest_args=(-q -ra --tb=short)
if [[ -n "${CANDIDATE_JUNIT_XML:-}" ]]; then
  pytest_args+=("--junitxml=$CANDIDATE_JUNIT_XML")
fi
# Keep this shell alive so its EXIT trap removes the fixture after pytest.
"$python_bin" -m pytest "${pytest_args[@]}" "$repo_root/python/tests/test_ray_datasource.py"
