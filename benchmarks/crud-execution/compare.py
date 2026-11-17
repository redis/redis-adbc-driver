"""Audit complete native CRUD/query A1/B1/B2/A2 windows and compare all cases.

Offline only. Pooled and forward/reverse results retain each build's raw Redis
controls. Valid evidence remains exportable when the declared target is not met.
"""
import argparse
import csv
import hashlib
import json
import math
from pathlib import Path
import statistics
import sys

TABLE_MODES = Path(__file__).resolve().parents[1] / 'table-modes'
sys.path.insert(0, str(TABLE_MODES))
import chart_data

ORDER = ('A1', 'B1', 'B2', 'A2')
BUILD_KEYS = {'driver_source_commit', 'driver_sha256', 'driver_version'}
CASE_KEYS = ('suite', 'operation', 'mode', 'clients')
SETTINGS = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}
FIXED_METADATA = {
    'automatic_retries': 0, 'protocol': 3, 'tls': True, 'repetitions': 3,
    'operations': 32, 'queries': 8, 'point_rows': 1000, 'olap_rows': 100000,
    'columns': 4, 'suite': 'all',
    'generator': 'id=1..N; grp=id%16; amount=id*3; label=row-{id:08d}',
    'timing_scope': 'Persistent per-thread clients; execute plus complete typed Arrow result or acknowledged write. Setup/teardown and independent validation excluded. Closed-loop fixed-operation trials, not saturated capacity.',
    'addressing': 'Managed known internal row IDs; unmanaged/raw known application keys. Mapping lookup excluded; no multi-statement transactions.',
}
PACKAGE_KEYS = {'pyarrow', 'adbc-driver-manager', 'redis', 'hiredis'}
TARGET_REDUCTION_PERCENT = 10.0


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()



def validate(data):
    meta = data['metadata']
    assert set(meta) == set(FIXED_METADATA) | BUILD_KEYS | {'harness_sha256', 'packages'}, 'Unexpected or missing benchmark metadata'
    for key, expected in FIXED_METADATA.items():
        assert type(meta[key]) is type(expected) and meta[key] == expected, 'Wrong fixture/policy: ' + key
    for key, length in [('driver_source_commit', 40), ('driver_sha256', 64), ('harness_sha256', 64)]:
        assert len(meta[key]) == length and all(c in '0123456789abcdef' for c in meta[key]), key
    assert meta['driver_version'].startswith('v') and len(meta['driver_version']) > 1
    assert meta['harness_sha256'] == digest(TABLE_MODES / 'scenarios.py'), 'Measured scenario source differs from frozen local harness'
    assert set(meta['packages']) == PACKAGE_KEYS
    assert all(isinstance(value, str) and value for value in meta['packages'].values())
    assert data['final_qpf'] == SETTINGS
    # Existing strict independent audit checks the full matrix, every latency
    # array, warmup/repetition, finite duration, type/value-validation marker and
    # reported per-trial arithmetic. Do not replace this with summary trust.
    summary = chart_data.scenario_summary(data)
    assert type(data['failures']) is list and data['failures'] == []
    for trial in data['trials']:
        assert trial['validated'] is True and type(trial['warmup']) is bool
        assert type(trial['seconds']) in (int, float) and math.isfinite(trial['seconds'])
        assert all(type(trial[key]) is int for key in ['clients', 'repeat', 'fixture_rows', 'columns', 'operations'])
    assert len(data['trials']) == 168 and len(summary) == 42
    return summary


def grouped(data):
    groups = {}
    for window in data:
        for trial in window['trials']:
            if not trial['warmup']:
                key = tuple(trial[name] for name in CASE_KEYS)
                groups.setdefault(key, []).append(trial)
    return groups


def statistics_of(trials):
    values = [value for trial in trials for value in trial['latency_ns']]
    total_ops = sum(trial['operations'] for trial in trials)
    total_seconds = sum(trial['seconds'] for trial in trials)
    return {'p50_ms': statistics.median(values) / 1e6,
            'p95_ms': chart_data.percentile(values, .95) / 1e6,
            'mean_ms': statistics.mean(values) / 1e6,
            'min_ms': min(values) / 1e6, 'max_ms': max(values) / 1e6,
            'median_trial_operations_per_second': statistics.median(trial['operations_per_second'] for trial in trials),
            'summed_trial_operations_per_second': total_ops / total_seconds,
            'summed_trial_seconds': total_seconds, 'operations': total_ops,
            'latency_samples': len(values), 'measured_trials': len(trials)}


def compare_groups(baseline, candidate):
    assert set(baseline) == set(candidate) and len(baseline) == 42
    a = {key: statistics_of(trials) for key, trials in baseline.items()}
    b = {key: statistics_of(trials) for key, trials in candidate.items()}
    rows = []
    for key in sorted(a):
        suite, operation, mode, clients = key
        raw_key = (suite, operation, 'redis_py', clients)
        before, after = a[key], b[key]
        row = dict(zip(CASE_KEYS, key))
        row.update(baseline=before, candidate=after,
                   p50_baseline_over_candidate=before['p50_ms'] / after['p50_ms'],
                   p50_elapsed_reduction_percent=(1 - after['p50_ms'] / before['p50_ms']) * 100,
                   p95_elapsed_change_percent=(after['p95_ms'] / before['p95_ms'] - 1) * 100,
                   mean_elapsed_change_percent=(after['mean_ms'] / before['mean_ms'] - 1) * 100,
                   summed_trial_throughput_ratio=after['summed_trial_operations_per_second'] / before['summed_trial_operations_per_second'],
                   baseline_redis_py_p50_ms=a[raw_key]['p50_ms'],
                   candidate_redis_py_p50_ms=b[raw_key]['p50_ms'],
                   baseline_elapsed_over_redis_py=before['p50_ms'] / a[raw_key]['p50_ms'],
                   candidate_elapsed_over_redis_py=after['p50_ms'] / b[raw_key]['p50_ms'])
        rows.append(row)
    return rows


def compare(windows, *, baseline_source=None, candidate_source=None, caller_paths=False):
    assert set(windows) == set(ORDER), 'Require all four declared ABBA windows'
    caller_metadata = {}
    audited = windows
    if caller_paths:
        # Copy only metadata; never mutate supplied evidence or relax the full
        # scenario auditor. A caller experiment freezes one native build.
        audited = {}
        for name in ORDER:
            meta = dict(windows[name]['metadata'])
            execution = meta.pop('mutation_execution')
            wrapper = meta.pop('no_result_wrapper_sha256')
            assert execution == ('execute' if name.startswith('A') else 'executemany'), 'Wrong caller path/window'
            assert wrapper == digest(Path(__file__).with_name('no_result.py')), 'Wrong caller wrapper'
            assert meta['packages']['adbc-driver-manager'] == '1.13.0', 'Reinspect caller APIs before changing manager'
            caller_metadata[name] = dict(mutation_execution=execution, no_result_wrapper_sha256=wrapper)
            audited[name] = {**windows[name], 'metadata': meta}
    window_summaries = {name: validate(audited[name]) for name in ORDER}
    a1, b1, b2, a2 = (windows[name]['metadata'] for name in ORDER)
    assert a1 == a2, 'Baseline source/library/version/harness/fixture/packages changed between A windows'
    assert b1 == b2, 'Candidate source/library/version/harness/fixture/packages changed between B windows'
    if caller_paths:
        assert audited['A1']['metadata'] == audited['B1']['metadata'], 'Caller comparison requires the same native build/fixture/packages'
    else:
        assert {key: value for key, value in a1.items() if key not in BUILD_KEYS} == {
            key: value for key, value in b1.items() if key not in BUILD_KEYS}, 'Builds differ in benchmark/harness/fixture/packages'
        assert a1['driver_source_commit'] != b1['driver_source_commit'], 'No distinct candidate source'
        assert a1['driver_sha256'] != b1['driver_sha256'], 'No distinct candidate native library'
    if baseline_source is not None:
        assert a1['driver_source_commit'] == baseline_source
    if candidate_source is not None:
        assert b1['driver_source_commit'] == candidate_source
    pooled = compare_groups(grouped([windows['A1'], windows['A2']]), grouped([windows['B1'], windows['B2']]))
    pairs = []
    for name, base, candidate, order in [('forward', 'A1', 'B1', ['baseline', 'candidate']),
                                          ('reverse', 'A2', 'B2', ['candidate', 'baseline'])]:
        pairs.append({'name': name, 'baseline_window': base, 'candidate_window': candidate,
                      'execution_order': order,
                      'rows': compare_groups(grouped([windows[base]]), grouped([windows[candidate]]))})
    target_rows = []
    operations = ('insert', 'update', 'delete') if caller_paths else ('point_read',)
    for mode, operation in ((m, op) for m in ('managed', 'unmanaged') for op in operations):
        row = next(row for row in pooled if row['suite'] == 'oltp' and row['operation'] == operation and row['mode'] == mode and row['clients'] == 1)
        target_rows.append({'mode': mode, 'operation': operation, 'clients': 1,
                            'baseline_p50_ms': row['baseline']['p50_ms'],
                            'candidate_p50_ms': row['candidate']['p50_ms'],
                            'elapsed_reduction_percent': row['p50_elapsed_reduction_percent'],
                            'met': row['candidate']['p50_ms'] <= row['baseline']['p50_ms'] * .9})
    return {
        'schema_version': 1, 'execution_order': list(ORDER),
        'comparison_kind': 'no-result-caller' if caller_paths else 'driver',
        'caller_metadata': caller_metadata,
        'scope': 'All four complete native scenario windows, including raw Redis controls. P50/mean pool measured operation samples; p95 is nearest rank. Ratios use unrounded samples/statistics. Pair arrows always mean baseline to candidate, including reverse execution.',
        'throughput_scope': 'Summed-trial throughput is total completed measured operations divided by summed trial wall times; median-trial throughput is the median of per-trial rates. Both are finite closed-loop completion rates including synchronization/scheduling, not saturated capacity. Per-operation means are not their inverse with concurrent clients.',
        'limits': ['These finite windows do not establish confidence intervals, production capacity or server-CPU attribution.',
                   'Fresh client objects are outside clocks; lazy first-operation transport work remains included.',
                   'Metadata automatic_retries=0 declares no caller replay, not measured wire retry/reconnect telemetry.',
                   'Driver/CLI/image/runtime/server and helper freeze provenance must accompany the raw records; JSON metadata alone does not authenticate executed binaries.',
                   'Window labels declare execution order; retain independent sequencing records with provenance.'],
        'settings': SETTINGS, 'builds': {'baseline': a1, 'candidate': b1},
        'windows': {name: {'build': 'baseline' if name.startswith('A') else 'candidate',
                           'trial_records': 168, 'warmup_records': 42, 'measured_records': 126,
                           'summary': window_summaries[name]} for name in ORDER},
        'pooled': pooled, 'pairs': pairs,
        'target': {'requirement': ('At least 10% less pooled one-client INSERT, UPDATE and DELETE p50 elapsed time independently in each driver mode (six cases); candidate <= 0.9 * baseline.' if caller_paths else 'At least 10% less pooled one-client point-read p50 elapsed time independently in managed and unmanaged modes; candidate <= 0.9 * baseline.'),
                   'required_elapsed_reduction_percent': TARGET_REDUCTION_PERCENT,
                   'cases': target_rows, 'met': all(row['met'] for row in target_rows)},
        'local_audit_source_sha256': {'compare.py': digest(Path(__file__)),
                                      'chart_data.py': digest(TABLE_MODES / 'chart_data.py'),
                                      'scenarios.py': digest(TABLE_MODES / 'scenarios.py'),
                                      'table-modes/run.py': digest(TABLE_MODES / 'run.py')},
    }


def flat(row):
    result = {key: row[key] for key in CASE_KEYS}
    for build in ['baseline', 'candidate']:
        result.update({build + '_' + key: value for key, value in row[build].items()})
    result.update({key: value for key, value in row.items() if key not in CASE_KEYS and key not in ['baseline', 'candidate']})
    return result


def markdown(result):
    text = '# Native CRUD/query ABBA comparison\n\n'
    text += '672 validated trial records: 168 each in A1/B1/B2/A2, including 168 warmups and 504 measured records. '
    text += 'Every 42-case matrix and all raw Redis control series are retained.\n\n'
    text += 'Target: **' + ('MET' if result['target']['met'] else 'NOT MET') + '** — ' + result['target']['requirement'] + ' '
    text += 'A valid comparison does not imply adoption; tail/other-case regressions remain below.\n\n'
    text += result['throughput_scope'] + '\n\n'
    for title, rows in [('Pooled measured samples', result['pooled'])] + [(pair['name'].capitalize() + ' pair (baseline → candidate)', pair['rows']) for pair in result['pairs']]:
        text += '## ' + title + '\n\n'
        text += '| Scenario | Mode | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py p50 |\n'
        text += '|---|---|---:|---:|---:|---:|---:|---:|---:|\n'
        for row in rows:
            a, b = row['baseline'], row['candidate']
            text += f"| {row['operation']} | {row['mode']} | {row['clients']} | {a['p50_ms']:.4f}→{b['p50_ms']:.4f} | {a['p95_ms']:.4f}→{b['p95_ms']:.4f} | {a['mean_ms']:.4f}→{b['mean_ms']:.4f} | {row['p50_elapsed_reduction_percent']:.2f}% | {a['summed_trial_operations_per_second']:.2f}→{b['summed_trial_operations_per_second']:.2f} | {row['candidate_elapsed_over_redis_py']:.3f}× |\n"
        text += '\n'
    return text


def write_outputs(result, output):
    output.mkdir(parents=True, exist_ok=False)
    (output / 'comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    (output / 'report.md').write_text(markdown(result))
    for name, rows in [('comparison.csv', [flat(row) for row in result['pooled']]),
                       ('pairs.csv', [{'pair': pair['name'], **flat(row)} for pair in result['pairs'] for row in pair['rows']])]:
        with (output / name).open('w', newline='') as file:
            writer = csv.DictWriter(file, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ORDER:
        parser.add_argument('--' + name.lower(), type=Path, required=True)
    parser.add_argument('--baseline-source', default='3b42245402a2d7cf6f363faf7ac713cd36621f90')
    parser.add_argument('--candidate-source')
    parser.add_argument('--caller-paths', action='store_true', help='Same native binary: explicit execute versus executemany(None) mutation callers')
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--require-target', action='store_true')
    args = parser.parse_args()
    paths = {name: getattr(args, name.lower()) for name in ORDER}
    windows = {name: json.loads(path.read_text()) for name, path in paths.items()}
    result = compare(windows, baseline_source=args.baseline_source, candidate_source=args.candidate_source, caller_paths=args.caller_paths)
    result['input_sha256'] = {name: digest(path) for name, path in paths.items()}
    write_outputs(result, args.out)
    print('PASS: 672 validated records, 42 pooled cases, 84 forward/reverse pair cases; target', 'MET' if result['target']['met'] else 'NOT MET')
    if args.require_target and not result['target']['met']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
