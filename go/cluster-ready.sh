#!/bin/sh
# A cluster is ready only when every primary can serve commands with Search.
# Checking just the seed can admit tests while another primary is CLUSTERDOWN.
set -eu
if [ "$#" -eq 0 ]; then
  set -- 7001 7002 7003
fi
for port do
  info=$(redis-cli --raw -e -p "$port" CLUSTER INFO)
  if ! printf '%s\n' "$info" | tr -d '\r' | grep -qx 'cluster_state:ok'; then
    echo "Redis cluster node $port is not ready" >&2
    exit 1
  fi
  redis-cli --raw -e -p "$port" FT._LIST >/dev/null
done
