#!/usr/bin/env python3
"""Audit and summarize exact frozen native ABBA files without removing failures."""
import argparse
import collections
import csv
import json
import math
from pathlib import Path
import statistics

LABELS = ('A1', 'B1', 'B2', 'A2')
OPERATIONS = ('insert', 'update', 'delete')
MODES = ('managed', 'redis_py')
CASES = [(op, mode, clients) for op in OPERATIONS for mode in MODES for clients in (1, 4)]
EXPECTED = {(rep, op, mode, clients) for rep in (-1, 0, 1, 2) for op, mode, clients in CASES}


def percentile(values, fraction):
    values = sorted(values)
    return values[math.ceil(len(values) * fraction) - 1]


def audit(profile, build, hashes):
    assert profile['status'] == 'complete' and not profile['failures']
    metadata = profile['metadata']
    assert metadata['driver_source_commit'] == build['commit']
    assert metadata['driver_sha256'] == build['library_sha256']
    assert metadata['driver_version'] == build['version']
    assert metadata['harness_sha256'] == hashes['benchmarks/table-modes/scenarios.py']
    assert metadata['selection_wrapper_sha256'] == hashes['native_run.py']
    assert metadata['writer_admission_queue_requested'] is build['option_requested']
    assert metadata['writer_admission_queue_verified'] is build['option_requested']
    assert metadata['protocol'] == 3 and metadata['tls'] is True
    assert (metadata['repetitions'], metadata['operations'], metadata['point_rows'], metadata['columns']) == (3, 32, 1000, 4)
    assert set(metadata['selected_operations']) == set(OPERATIONS)
    assert set(metadata['selected_modes']) == set(MODES)
    seen = collections.Counter((t['repeat'], t['operation'], t['mode'], t['clients']) for t in profile['trials'])
    assert len(profile['trials']) == 48 and set(seen) == EXPECTED and all(n == 1 for n in seen.values())
    for trial in profile['trials']:
        assert trial['suite'] == 'oltp' and trial['status'] == 'validated' and trial['validated'] is True
        assert trial['warmup'] is (trial['repeat'] == -1)
        assert trial['operations'] == 32 and len(trial['latency_ns']) == 32
        assert all(isinstance(v, int) and v > 0 for v in trial['latency_ns'])
        assert math.isfinite(trial['seconds']) and trial['seconds'] > 0
        assert math.isclose(trial['median_latency_ms'], statistics.median(trial['latency_ns']) / 1e6)
        assert math.isclose(trial['p95_latency_ms'], percentile(trial['latency_ns'], .95) / 1e6)
        assert math.isclose(trial['operations_per_second'], 32 / trial['seconds'])
    assert profile['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def summarize(profiles, case):
    trials = [t for p in profiles for t in p['trials'] if not t['warmup'] and (t['operation'], t['mode'], t['clients']) == case]
    values = [v for t in trials for v in t['latency_ns']]
    seconds = sum(t['seconds'] for t in trials)
    return {'measured_trials': len(trials), 'operations': len(values), 'seconds': seconds,
            'operations_per_second': len(values) / seconds,
            'p50_ms': statistics.median(values) / 1e6, 'p95_ms': percentile(values, .95) / 1e6,
            'p99_ms': percentile(values, .99) / 1e6, 'mean_ms': statistics.mean(values) / 1e6,
            'max_ms': max(values) / 1e6,
            'trial_seconds': [t['seconds'] for t in trials]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bundle', type=Path, default=Path(__file__).parent)
    args = parser.parse_args()
    bundle = args.bundle
    freeze = json.loads((bundle / 'frozen-builds.json').read_text())
    profiles = {label: json.loads((bundle / (label + '.json')).read_text()) for label in LABELS}
    for label in LABELS:
        mode = 'baseline' if label.startswith('A') else 'candidate'
        audit(profiles[label], freeze['builds'][mode], freeze['harness_sha256'])
    result = {'complete_validated_comparison': True, 'window_order': list(LABELS),
              'records': 192, 'warmup_records': 48, 'measured_records': 144,
              'total_acknowledged_operations': 6144, 'measured_operations': 4608,
              'scope': 'Linux/arm64 normal Python/ADBC managed writes vs redis-py; baseline legacy/default vs explicit opt-in candidate. Full original typed/final-state oracles completed. Finite closed-loop trial completion, not saturated capacity or server CPU.',
              'source_builds': freeze['builds'],
              'retry_metadata_scope': 'Original automatic_retries=0 labels are preserved. This is not a measured wire-level retry/reconnect counter; see retry-policy-audit.json.',
              'comparisons': []}
    groups = {'pooled_ABBA': (['A1', 'A2'], ['B1', 'B2']),
              'forward_A1_B1': (['A1'], ['B1']),
              'reverse_B2_A2': (['A2'], ['B2'])}
    csv_rows = []
    for scope, (baseline_labels, candidate_labels) in groups.items():
        for case in CASES:
            baseline = summarize([profiles[x] for x in baseline_labels], case)
            candidate = summarize([profiles[x] for x in candidate_labels], case)
            row = {'comparison_scope': scope, 'operation': case[0], 'mode': case[1], 'clients': case[2],
                   'baseline_windows': baseline_labels, 'candidate_windows': candidate_labels,
                   'baseline': baseline, 'candidate': candidate,
                   'throughput_C_over_B': candidate['operations_per_second'] / baseline['operations_per_second'],
                   'p50_C_over_B': candidate['p50_ms'] / baseline['p50_ms'],
                   'p95_C_over_B': candidate['p95_ms'] / baseline['p95_ms'],
                   'mean_C_over_B': candidate['mean_ms'] / baseline['mean_ms']}
            result['comparisons'].append(row)
            csv_rows.append({k: row[k] for k in ('comparison_scope', 'operation', 'mode', 'clients')} |
                            {f'{mode}_{key}': values[key] for mode, values in (('baseline', baseline), ('candidate', candidate))
                             for key in ('measured_trials', 'operations', 'seconds', 'operations_per_second', 'p50_ms', 'p95_ms', 'p99_ms', 'mean_ms', 'max_ms')} |
                            {k: row[k] for k in ('throughput_C_over_B', 'p50_C_over_B', 'p95_C_over_B', 'mean_C_over_B')})
    (bundle / 'comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    with (bundle / 'comparison.csv').open('w', newline='') as output:
        writer = csv.DictWriter(output, fieldnames=list(csv_rows[0]), lineterminator="\n")
        writer.writeheader()
        writer.writerows(csv_rows)
    print('Validated all 192 records/6144 operations; retained all 36 pooled/per-pair case comparisons and raw controls.')


if __name__ == '__main__':
    main()
