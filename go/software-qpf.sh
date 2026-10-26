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

# Shared by the owned Redis Software RAM and Flex test fixtures.
# The caller supplies call(), wait_for(), api and uid.
software_qpf_4x_ready() {
    call -f "$api/bdbs/$uid?fields=status,query_performance_factor,search,sched_policy,conns" | jq -e '
        .status == "active"
        and .query_performance_factor.active == true
        and .query_performance_factor.scaling_factor == 4
        and .search["search-workers"] == 6
        and .sched_policy == "mnp" and .conns == 32
    ' >/dev/null
}

software_qpf_4x() {
    if ! software_qpf_4x_ready; then
        software_qpf_4x_request() {
            call -f -o /dev/null -X PUT "$api/bdbs/$uid" \
                -d '{"query_performance_factor":{"active":true,"scaling_factor":4}}'
        }
        wait_for '4x QPF configuration acceptance' software_qpf_4x_request
    fi
    wait_for 'effective 4x QPF (6 Search workers, mnp routing, 32 connections)' software_qpf_4x_ready
}
