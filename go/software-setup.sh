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

# Starts the default disposable Redis Software RAM test database with Search.
# Requires Docker Compose, curl and jq. Fixed credentials and a trial license
# are for local testing only; both published ports bind to localhost.
set -euo pipefail
cd "$(dirname "$0")"
for tool in docker curl jq; do
    command -v "$tool" >/dev/null || { echo "Required tool not found: $tool" >&2; exit 1; }
done
api_port=${SOFTWARE_API_PORT:-19443}
db_port=${SOFTWARE_DB_PORT:-6379}
admin_user=${SOFTWARE_ADMIN_USER:-admin@example.com}
admin_pass=${SOFTWARE_ADMIN_PASSWORD:-adbc-software-test}
api="https://localhost:$api_port/v1"
call() { curl -sSk -u "$admin_user:$admin_pass" -H 'Content-Type: application/json' "$@"; }
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
if [ "$(state)" = idle ]; then
    jq -n --arg user "$admin_user" --arg pass "$admin_pass" '{
        action: "create_cluster", cluster: {name: "adbc-test.local"}, node: {},
        credentials: {username: $user, password: $pass}
    }' | curl -sSkf -H 'Content-Type: application/json' -X POST "$api/bootstrap/create_cluster" -d @- >/dev/null
fi
bootstrapped() {
    local s
    s=$(state)
    if [ "$s" = failed ]; then
        call "$api/bootstrap" >&2
        exit 1
    fi
    [ "$s" = completed ] && call -f -o /dev/null "$api/cluster"
}
wait_for 'Redis Software cluster' bootstrapped
uid=$(call -f "$api/bdbs" | jq -r '[.[] | select(.name == "adbc-test") | .uid][0] // empty')
if [ -z "$uid" ]; then
    # Redis 8 includes Search; an explicit module_list is unnecessary.
    body='{"name":"adbc-test","type":"redis","redis_version":"8.6","port":12000,"memory_size":1073741824,"replication":false,"data_persistence":"disabled","eviction_policy":"noeviction"}'
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
call -f "$api/bdbs/$uid" | jq -e '.port == 12000 and .bigstore != true and .replication == false' >/dev/null
ready() { docker compose exec -T redis-software /opt/redislabs/bin/redis-cli -e -p 12000 FT._LIST >/dev/null; }
wait_for 'Search endpoint' ready
call -f "$api/bdbs/$uid" | jq -r '"Redis Software database: Redis \(.version), modules \([.module_list[].module_name] | join(", "))"' >&2
echo "REDIS_URI=redis://localhost:$db_port/0"
