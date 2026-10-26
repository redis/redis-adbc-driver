#!/usr/bin/env bash
# Owned TLS-only OSS Cluster. Caller removes this named container/private dir.
# Usage: start_partition_tls_cluster.sh NEW_DIRECTORY CONTAINER_NAME FIRST_PORT
set -euo pipefail
umask 077
fixture_dir=$1
fixture_container=$2
fixture_first=$3
if ! [[ "$fixture_first" =~ ^[1-9][0-9]{3,4}$ ]] || (( fixture_first < 1024 || fixture_first > 55533 )); then
  printf '%s\n' 'First TLS port must be an integer between 1024 and 55533.' >&2
  exit 2
fi
bash "$(dirname "$0")/create_partition_tls.sh" "$fixture_dir" "$fixture_first"
fixture_dir=$(cd "$fixture_dir" && pwd)
# Cluster-bus peers authenticate as TLS clients as well as servers. Keep a
# separately signed clientAuth leaf; the shared server leaf is serverAuth-only.
openssl req -newkey rsa:2048 -nodes -keyout "$fixture_dir/cluster-client.key" \
  -out "$fixture_dir/cluster-client.csr" -subj '/CN=partition-cluster-client' >/dev/null 2>&1
printf '%s\n' 'subjectAltName=DNS:localhost,IP:127.0.0.1' 'extendedKeyUsage=clientAuth' \
  'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' \
  > "$fixture_dir/cluster-client.ext"
openssl x509 -req -in "$fixture_dir/cluster-client.csr" -CA "$fixture_dir/ca.crt" \
  -CAkey "$fixture_dir/ca.key" -CAcreateserial -out "$fixture_dir/cluster-client.crt" \
  -days 1 -extfile "$fixture_dir/cluster-client.ext" >/dev/null 2>&1
for port in "$fixture_first" "$((fixture_first+1))" "$((fixture_first+2))"; do
  cat "$fixture_dir/redis.conf" > "$fixture_dir/redis-$port.conf"
  cat >> "$fixture_dir/redis-$port.conf" <<CONFIG
tls-port $port
tls-cluster yes
tls-client-cert-file /tls/cluster-client.crt
tls-client-key-file /tls/cluster-client.key
cluster-enabled yes
cluster-announce-ip 127.0.0.1
cluster-announce-tls-port $port
cluster-config-file /tmp/adbc-partition-cluster/$port/nodes.conf
dir /tmp/adbc-partition-cluster/$port
daemonize yes
logfile /tmp/adbc-partition-cluster/$port/redis.log
loadmodule /usr/local/lib/redis/modules/redisearch.so
CONFIG
done
fixture_id=$(docker run -d --name "$fixture_container" --label codex.fixture=partition-tls-cluster \
  --user "$(id -u):$(id -g)" \
  -p "127.0.0.1:$fixture_first:$fixture_first" \
  -p "127.0.0.1:$((fixture_first+1)):$((fixture_first+1))" \
  -p "127.0.0.1:$((fixture_first+2)):$((fixture_first+2))" \
  -v "$fixture_dir:/tls:ro" -e "FIRST_PORT=$fixture_first" redis:8.6.2 sh -ec '
    export REDISCLI_AUTH=$(cat /tls/admin.password)
    for port in "$FIRST_PORT" "$((FIRST_PORT+1))" "$((FIRST_PORT+2))"; do
      mkdir -p "/tmp/adbc-partition-cluster/$port"
      redis-server "/tls/redis-$port.conf"
      ready=false
      for attempt in $(seq 1 50); do
        if redis-cli --tls --cacert /tls/ca.crt -e -p "$port" PING >/dev/null 2>&1; then ready=true; break; fi
        sleep 0.2
      done
      "$ready"
    done
    redis-cli --tls --cacert /tls/ca.crt --cluster create \
      "127.0.0.1:$FIRST_PORT" "127.0.0.1:$((FIRST_PORT+1))" "127.0.0.1:$((FIRST_PORT+2))" \
      --cluster-replicas 0 --cluster-yes >/dev/null
    exec tail -f /dev/null')
python3 - "$fixture_dir" "$fixture_id" "$fixture_first" <<'PY'
import json,sys
from pathlib import Path
folder,container,first=sys.argv[1:]
(folder:=Path(folder)).joinpath('cluster.json').write_text(json.dumps({
    'deployment':'owned OSS Cluster TLS', 'container_id':container,
    'ports':list(range(int(first),int(first)+3))},indent=2)+'\n')
PY
for attempt in $(seq 1 45); do
  if docker exec "$fixture_id" sh -ec '
    export REDISCLI_AUTH=$(cat /tls/admin.password)
    for port in "$FIRST_PORT" "$((FIRST_PORT+1))" "$((FIRST_PORT+2))"; do
      redis-cli --tls --cacert /tls/ca.crt --raw -e -p "$port" CLUSTER INFO | tr -d "\r" | grep -qx cluster_state:ok
      redis-cli --tls --cacert /tls/ca.crt -e -p "$port" CONFIG SET search-max-aggregate-results 137 >/dev/null
      redis-cli --tls --cacert /tls/ca.crt -e -p "$port" FT._LIST >/dev/null
    done' >/dev/null 2>&1; then
    printf '%s\n' 'Owned TLS-only three-primary Redis Cluster ready; Search and cap 137 on every primary.'
    exit 0
  fi
  sleep 1
done
printf '%s\n' 'TLS Cluster fixture did not become ready; remove the owned container and private directory.' >&2
exit 1
