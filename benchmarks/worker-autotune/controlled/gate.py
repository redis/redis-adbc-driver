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

"""Predeclared readiness gate; missing trials are infrastructure failures."""
import collections
import json
import statistics
from pathlib import Path

POLICIES = ['serial', 'cpu-fixed', 'headroom-fixed', 'tuned']

def evaluate(records, manifest):
    groups = collections.defaultdict(lambda: collections.defaultdict(list))
    for r in records:
        key = (r['deployment'], r['rows'], r['GOMAXPROCS'], r['condition'], r['arrival_mode'], r['concurrency'])
        groups[key][r['policy']].append(r)
    expected = {(s['deployment'], s['rows'], s['cpu'], s['condition'], s['mode'], c)
                for s in manifest['scenarios'] for c in s['concurrencies']}
    infrastructure, comparisons = [], []
    for key in sorted(expected | set(groups)):
        policies = groups[key]
        required = POLICIES + (['legacy-tuned'] if manifest.get('baseline_comparison') and key[0] == 'oss' and key[2] == 4 else [])
        if key not in expected or set(policies) - set(required) or any(len(policies[p]) != manifest['repetitions'] or
            {r['repetition'] for r in policies[p]} != set(range(manifest['repetitions'])) for p in required):
            infrastructure.append(dict(case=key, error='missing, duplicate, or unexpected policy/repetition'))
            continue
        summary = {}
        for policy in required:
            trials = policies[policy]
            summary[policy] = {}
            for metric in ['queries_per_second', 'p95_query_seconds', 'client_cpu_seconds', 'client_peak_heap_bytes',
                           'client_process_peak_rss_bytes', 'redis_network_bytes_per_second', 'redis_cpu_seconds', 'redis_memory_bytes']:
                values = [r[metric] for r in trials]
                summary[policy][metric] = dict(median=statistics.median(values), minimum=min(values), maximum=max(values))
        med = lambda policy, metric: summary[policy][metric]['median']
        # Select the best measured fixed throughput; compare latency with that SAME
        # policy, not a cherry-picked independently fastest latency configuration.
        best = max(POLICIES[:-1], key=lambda p: med(p, 'queries_per_second'))
        steady_qps = med('tuned', 'queries_per_second') / med(best, 'queries_per_second')
        steady_tail = med('tuned', 'p95_query_seconds') / med(best, 'p95_query_seconds')
        cold_qps = statistics.median(r['cold_learning']['queries_per_second'] for r in policies['tuned']) / statistics.median(r['cold_learning']['queries_per_second'] for r in policies['cpu-fixed'])
        cold_tail = statistics.median(r['cold_learning']['p95_query_seconds'] for r in policies['tuned']) / statistics.median(r['cold_learning']['p95_query_seconds'] for r in policies['cpu-fixed'])
        bounds = manifest['bounds']
        passed = steady_qps >= bounds['steady_qps_min'] and steady_tail <= bounds['steady_p95_max'] and cold_qps >= bounds['cold_qps_min'] and cold_tail <= bounds['cold_p95_max']
        legacy = dict(qps_ratio=med('tuned', 'queries_per_second') / med('legacy-tuned', 'queries_per_second'), p95_ratio=med('tuned', 'p95_query_seconds') / med('legacy-tuned', 'p95_query_seconds')) if 'legacy-tuned' in summary else None
        comparisons.append(dict(legacy_comparison=legacy, case=key, best_fixed=best, steady_qps_ratio=steady_qps, steady_p95_ratio=steady_tail,
                                cold_qps_ratio=cold_qps, cold_p95_ratio=cold_tail, passed=passed, policies=summary))
    return dict(status='infrastructure' if infrastructure else 'pass' if all(c['passed'] for c in comparisons) and comparisons else 'regression',
                infrastructure=infrastructure, cases=comparisons,
                note='Medians and full min/max spread across independent repeated trials; these are not confidence intervals. Warmed windows can still include probes. Local allocation is not a global optimum guarantee.')

def write_report(output, result):
    Path(output, 'gate.json').write_text(json.dumps(result, indent=2) + '\n')
    lines = ['# Controlled autotuning evaluation', '', f"Gate status: **{result['status']}**", '', result['note'], '',
             'Cold bounds: QPS >= 85% and p95 <= 120% of the CPU-sized baseline. Warmed bounds: QPS >= 90% and p95 <= 115% of the highest-throughput measured fixed policy.', '',
             '| Deployment / rows / CPU / link / arrivals / concurrency | Fixed reference | Warm QPS ratio | Warm p95 ratio | Cold QPS ratio | Cold p95 ratio | Pass |',
             '|---|---|---:|---:|---:|---:|---|']
    for c in result['cases']:
        lines.append('| ' + ' / '.join(map(str, c['case'])) + ' | ' + c['best_fixed'] + ' | ' + ' | '.join(f"{c[m]:.3f}" for m in ['steady_qps_ratio', 'steady_p95_ratio', 'cold_qps_ratio', 'cold_p95_ratio']) + f" | {c['passed']} |")
    for error in result['infrastructure']:
        lines.append(f"\nInfrastructure failure: {error}")
    Path(output, 'REPORT.md').write_text('\n'.join(lines) + '\n')
