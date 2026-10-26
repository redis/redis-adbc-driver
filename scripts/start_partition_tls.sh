#!/usr/bin/env bash
# Disposable TLS-only Redis fixture. Caller removes the named container and dir.
# Usage: start_partition_tls.sh NEW_DIRECTORY CONTAINER_NAME HOST_PORT
set -euo pipefail
fixture_dir=$1
fixture_container=$2
fixture_port=$3
bash "$(dirname "$0")/create_partition_tls.sh" "$fixture_dir" "$fixture_port"
fixture_dir=$(cd "$fixture_dir" && pwd)
fixture_password=$(cat "$fixture_dir/admin.password")
docker run -d --name "$fixture_container" --label codex.fixture=partition-tls \
  --user "$(id -u):$(id -g)" -p "127.0.0.1:$fixture_port:6379" \
  -v "$fixture_dir:/tls:ro" redis:8.6.2 redis-server /tls/redis.conf >/dev/null
for fixture_attempt in $(seq 1 30); do
  if docker exec -e "REDISCLI_AUTH=$fixture_password" "$fixture_container" \
    redis-cli --tls --cacert /tls/ca.crt -e PING >/dev/null 2>&1; then
    docker exec -e "REDISCLI_AUTH=$fixture_password" "$fixture_container" \
      redis-cli --tls --cacert /tls/ca.crt -e CONFIG SET search-max-aggregate-results 137 >/dev/null
    docker exec -e "REDISCLI_AUTH=$fixture_password" "$fixture_container" \
      redis-cli --tls --cacert /tls/ca.crt -e FT._LIST >/dev/null
    printf '%s\n' 'TLS-only Redis ready; verified fixture CA, Search, cap 137.'
    exit 0
  fi
  sleep 1
done
printf '%s\n' 'TLS fixture did not become ready' >&2
exit 1
