#!/usr/bin/env bash
# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Starts a disposable Redis Software RAM test database with Search and 4x QPF.
# Three shards (default) use TLS and explicit username/password authentication.
# The primary shards share one Software node and its proxy endpoint.
# Requires Docker Compose, curl and jq. Fixed credentials and a trial license
# are for local testing only; both published ports bind to localhost.
set -euo pipefail
# Run a command only after this exact owned endpoint's policy and QPF gates.
# Do not infer a guarantee from an empty CONFIG reply at the data proxy.
if [ "$#" -gt 0 ]; then
    if [ "$1" != --exec ] || [ "$#" -lt 2 ]; then
        echo "Usage: software-setup.sh [--exec command [args...]]" >&2
        exit 2
    fi
    shift
fi
cd "$(dirname "$0")"
for tool in docker curl jq openssl python3; do
    command -v "$tool" >/dev/null || { echo "Required tool not found: $tool" >&2; exit 1; }
done
api_port=${SOFTWARE_API_PORT:-19443}
db_port=${SOFTWARE_DB_PORT:-6379}
shards=${SOFTWARE_SHARDS:-3}
case $shards in
    1|3) ;;
    *) echo 'SOFTWARE_SHARDS must be 1 or 3' >&2; exit 1 ;;
esac
admin_user=${SOFTWARE_ADMIN_USER:-admin@example.com}
admin_pass=${SOFTWARE_ADMIN_PASSWORD:-adbc-software-test}
db_pass=${SOFTWARE_DB_PASSWORD:-adbc-enterprise-test}
tls_dir="$PWD/build/test-tls"
mkdir -p "$tls_dir"
umask 077
if [ ! -s "$tls_dir/server.key" ] || ! openssl x509 -checkend 86400 -noout -in "$tls_dir/ca.pem" >/dev/null 2>&1; then
    openssl req -x509 -newkey rsa:2048 -nodes -days 30 \
        -keyout "$tls_dir/server.key" -out "$tls_dir/ca.pem" \
        -subj '/CN=localhost' -addext 'subjectAltName=DNS:localhost,DNS:redis-software,IP:127.0.0.1' \
        -addext 'basicConstraints=critical,CA:TRUE' >&2
    chmod 600 "$tls_dir/server.key"
fi
# This public certificate must be readable by the container's Redis CLI user.
# Keep the private key at 0600; docker compose cp preserves the file modes.
chmod 644 "$tls_dir/ca.pem"
api="https://localhost:$api_port/v1"
call() {
    local method=GET i
    local args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
        if [ "${args[i]}" = -X ]; then method=${args[i+1]}; fi
    done
    # Container health can precede management readiness. Retry only reads and
    # idempotent updates, including temporary authentication failures while the
    # new cluster propagates credentials. A create POST must never be replayed.
    case $method in
        GET|PUT) set -- --retry 5 --retry-delay 1 --retry-max-time 30 --retry-all-errors "$@" ;;
    esac
    curl -sSk -u "$admin_user:$admin_pass" -H 'Content-Type: application/json' "$@" || {
        local status=$?
        echo "Management API $method request failed after readiness retries" >&2
        return "$status"
    }
}
wait_for() {
    local what=$1
    shift
    for _ in $(seq 120); do
        if "$@"; then return 0; fi
        sleep 2
    done
    echo "Timed out waiting for $what" >&2
    exit 1
}
docker compose up --detach --wait redis-software >&2
state() {
    { curl -sSk "$api/bootstrap" | jq -er '.bootstrap_status.state // empty' 2>/dev/null; } ||
        call -f "$api/bootstrap" | jq -er '.bootstrap_status.state // empty'
}
# The container is healthy before its API answers (the healthcheck accepts
# a 503), so the state is read until it can be, and an idle node is asked
# to create the cluster then, rather than once before the API is up.
requested=
bootstrapped() {
    local s
    s=$(state) || return 1
    case $s in
    completed) call -f -o /dev/null "$api/cluster" ;;
    failed)
        call "$api/bootstrap" >&2
        exit 1
        ;;
    idle)
        if [ -z "$requested" ]; then
            jq -n --arg user "$admin_user" --arg pass "$admin_pass" '{
                action: "create_cluster", cluster: {name: "adbc-test.local"}, node: {},
                credentials: {username: $user, password: $pass}
            }' | curl -sSkf -H 'Content-Type: application/json' -X POST "$api/bootstrap/create_cluster" -d @- >/dev/null &&
                requested=1
        fi
        return 1
        ;;
    *) return 1 ;;
    esac
}
wait_for 'Redis Software cluster' bootstrapped
# Use a locally trusted proxy certificate, with hostname verification enabled.
jq -n --rawfile cert "$tls_dir/ca.pem" --rawfile key "$tls_dir/server.key" \
    '{certificates: [{name: "proxy", certificate: $cert, key: $key}]}' | \
    call -f -X PUT "$api/cluster/certificates" -d @- >/dev/null
uid=$(call -f "$api/bdbs" | jq -r '[.[] | select(.name == "adbc-test") | .uid][0] // empty')
if [ -z "$uid" ]; then
    # Redis 8 includes Search; an explicit module_list is unnecessary.
    body=$(jq -n --argjson shards "$shards" --arg pass "$db_pass" '{
        name: "adbc-test", type: "redis", redis_version: "8.6", port: 12000,
        memory_size: 1073741824, replication: false,
        query_performance_factor: {active: true, scaling_factor: 4},
        tls_mode: "enabled", enforce_client_authentication: "disabled",
        authentication_redis_pass: $pass,
        data_persistence: "disabled", eviction_policy: "noeviction",
        shards_count: $shards, sharding: ($shards > 1), shards_placement: "dense"
    } + if $shards > 1 then {
        shard_key_regex: [{regex: ".*\\{(?<tag>.*)\\}.*"}, {regex: "(?<tag>.*)"}]
    } else {} end')
    create() {
        local reply code
        reply=$(call -w '\n%{http_code}' -X POST "$api/bdbs" -d "$body") || return 1
        code=${reply##*$'\n'}
        reply=${reply%$'\n'*}
        # A newly bootstrapped cluster briefly refuses database creation.
        if [ "$code" = 406 ]; then return 1; fi
        if [[ "$code" != 2* ]]; then
            echo "Database creation failed (HTTP $code): $reply" >&2
            exit 1
        fi
        uid=$(jq -er .uid <<<"$reply")
    }
    wait_for 'database creation' create
fi
active() { [ "$(call -f "$api/bdbs/$uid" | jq -r .status)" = active ]; }
wait_for 'RAM database' active
# Check the reused database rather than silently accepting a different setup.
if ! call -f "$api/bdbs/$uid" | jq -e --argjson shards "$shards" '
    .port == 12000 and .bigstore != true and .replication == false
    and .eviction_policy == "noeviction"
    and .shards_count == $shards and .sharding == ($shards > 1)
    and .tls_mode == "enabled"
' >/dev/null; then
    echo "Existing database does not match the requested $shards-shard RAM/noeviction topology; run make redis-down before changing topology." >&2
    exit 1
fi
source ./software-qpf.sh
software_qpf_4x
shards_ready() {
    call -f "$api/bdbs/$uid/shards" | jq -e --argjson shards "$shards" '
        [.[] | select(.role == "master" and .status == "active")] | length == $shards
    ' >/dev/null
}
wait_for 'primary shards' shards_ready
docker compose cp "$tls_dir/ca.pem" redis-software:/tmp/adbc-test-ca.pem >&2
ready() {
    docker compose exec -T -e REDISCLI_AUTH="$db_pass" redis-software \
        /opt/redislabs/bin/redis-cli --tls --cacert /tmp/adbc-test-ca.pem \
        --user default -e -p 12000 FT._LIST >/dev/null
}
wait_for 'Search endpoint' ready
call -f "$api/bdbs/$uid" | jq -r '"Redis Software database: Redis \(.version), \(.shards_count) shard(s), modules \([.module_list[].module_name] | join(", "))"' >&2
# Shell-safe exports for local commands, including custom passwords and ports.
SOFTWARE_TEST_PASSWORD="$db_pass" python3 - "$db_port" "$tls_dir/ca.pem" > build/test.env <<'PYENV'
import os, shlex, sys
from urllib.parse import quote
uri = f"rediss://default:{quote(os.environ['SOFTWARE_TEST_PASSWORD'], safe='')}@localhost:{sys.argv[1]}/0"
for key, value in {"REDIS_URI": uri, "SSL_CERT_FILE": sys.argv[2]}.items():
    print(f"export {key}={shlex.quote(value)}")
    if os.environ.get("GITHUB_ENV"):
        with open(os.environ["GITHUB_ENV"], "a") as env:
            env.write(f"{key}={value}\n")
PYENV
chmod 600 build/test.env
echo 'Test environment ready. Run: source go/build/test.env (or source build/test.env from go/).' >&2

if [ "$#" -gt 0 ]; then
    source build/test.env
    export REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed
    exec "$@"
fi
