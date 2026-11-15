"""Strict offline ABBA audit of the explicit point-read-only native target screen.

Never substitute these six cases for the full 42-case CRUD/query matrix.
"""
import argparse
import csv
import importlib.util
import json
from pathlib import Path

_spec = importlib.util.spec_from_file_location('_crud_full_compare', Path(__file__).with_name('compare.py'))
full = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(full)

EXTRA_METADATA = {'benchmark_kind', 'operation_selection', 'wrapper_sha256'}
CASES = {('oltp', 'point_read', mode, clients) for mode in ('managed', 'unmanaged', 'redis_py') for clients in (1, 4)}


def validate(data):
    meta = data['metadata']
    expected = dict(full.FIXED_METADATA, suite='oltp')
    assert set(meta) == set(expected) | full.BUILD_KEYS | {'harness_sha256', 'packages'} | EXTRA_METADATA
    for key, value in expected.items():
        assert type(meta[key]) is type(value) and meta[key] == value, key
    assert meta['benchmark_kind'] == 'point_read_target_screen'
    assert meta['operation_selection'] == ['point_read']
    assert meta['wrapper_sha256'] == full.digest(Path(__file__).with_name('point_read.py'))
    assert meta['harness_sha256'] == full.digest(full.TABLE_MODES / 'scenarios.py')
    for key, length in [('driver_source_commit', 40), ('driver_sha256', 64), ('harness_sha256', 64), ('wrapper_sha256', 64)]:
        assert len(meta[key]) == length and all(c in '0123456789abcdef' for c in meta[key]), key
    assert meta['driver_version'].startswith('v') and len(meta['driver_version']) > 1
    assert set(meta['packages']) == full.PACKAGE_KEYS
    assert all(isinstance(value, str) and value for value in meta['packages'].values())
    assert data['final_qpf'] == full.SETTINGS
    # Explicitly scope the established arithmetic/grid checker to this screen.
    # Always restore it, including malformed-input failure; the complete-suite
    # comparator must never accept this subset in the same interpreter.
    original = full.chart_data.POINT
    try:
        full.chart_data.POINT = ('point_read',)
        summary = full.chart_data.scenario_summary(data)
    finally:
        full.chart_data.POINT = original
    assert type(data['failures']) is list and data['failures'] == []
    assert len(data['trials']) == 24 and len(summary) == 6
    for trial in data['trials']:
        assert tuple(trial[key] for key in full.CASE_KEYS) in CASES
        assert trial['validated'] is True and type(trial['warmup']) is bool
        assert type(trial['seconds']) in (int, float)
        assert all(type(trial[key]) is int for key in ['clients', 'repeat', 'fixture_rows', 'columns', 'operations'])
    return summary


def compare_groups(baseline, candidate):
    assert set(baseline) == set(candidate) == CASES
    a = {key: full.statistics_of(rows) for key, rows in baseline.items()}
    b = {key: full.statistics_of(rows) for key, rows in candidate.items()}
    rows = []
    for key in sorted(CASES):
        raw = ('oltp', 'point_read', 'redis_py', key[-1])
        before, after = a[key], b[key]
        rows.append(dict(zip(full.CASE_KEYS, key)) | {
            'baseline': before, 'candidate': after,
            'p50_baseline_over_candidate': before['p50_ms'] / after['p50_ms'],
            'p50_elapsed_reduction_percent': (1 - after['p50_ms'] / before['p50_ms']) * 100,
            'p95_elapsed_change_percent': (after['p95_ms'] / before['p95_ms'] - 1) * 100,
            'mean_elapsed_change_percent': (after['mean_ms'] / before['mean_ms'] - 1) * 100,
            'summed_trial_throughput_ratio': after['summed_trial_operations_per_second'] / before['summed_trial_operations_per_second'],
            'baseline_redis_py_p50_ms': a[raw]['p50_ms'], 'candidate_redis_py_p50_ms': b[raw]['p50_ms'],
            'baseline_elapsed_over_redis_py': before['p50_ms'] / a[raw]['p50_ms'],
            'candidate_elapsed_over_redis_py': after['p50_ms'] / b[raw]['p50_ms']})
    return rows


def compare(windows, *, baseline_source=None, candidate_source=None):
    assert set(windows) == set(full.ORDER)
    summaries = {name: validate(windows[name]) for name in full.ORDER}
    a1, b1, b2, a2 = (windows[name]['metadata'] for name in full.ORDER)
    assert a1 == a2 and b1 == b2, 'A build or measurement configuration changed between its two windows'
    assert {k: v for k, v in a1.items() if k not in full.BUILD_KEYS} == {
        k: v for k, v in b1.items() if k not in full.BUILD_KEYS}, 'Builds differ outside source/library/version'
    assert a1['driver_source_commit'] != b1['driver_source_commit'] and a1['driver_sha256'] != b1['driver_sha256']
    if baseline_source is not None:
        assert a1['driver_source_commit'] == baseline_source
    if candidate_source is not None:
        assert b1['driver_source_commit'] == candidate_source
    pooled = compare_groups(full.grouped([windows['A1'], windows['A2']]), full.grouped([windows['B1'], windows['B2']]))
    pairs = []
    for name, base, candidate, order in [('forward', 'A1', 'B1', ['baseline', 'candidate']),
                                       ('reverse', 'A2', 'B2', ['candidate', 'baseline'])]:
        pairs.append({'name': name, 'baseline_window': base, 'candidate_window': candidate,
                      'execution_order': order,
                      'rows': compare_groups(full.grouped([windows[base]]), full.grouped([windows[candidate]]))})
    target = []
    for mode in ['managed', 'unmanaged']:
        row = next(row for row in pooled if row['mode'] == mode and row['clients'] == 1)
        target.append({'mode': mode, 'clients': 1, 'baseline_p50_ms': row['baseline']['p50_ms'],
                       'candidate_p50_ms': row['candidate']['p50_ms'],
                       'elapsed_reduction_percent': row['p50_elapsed_reduction_percent'],
                       'met': row['candidate']['p50_ms'] <= row['baseline']['p50_ms'] * .9})
    return {'schema_version': 1, 'benchmark_kind': 'point_read_target_screen',
            'scope': 'Explicit point-read-only selection: 96 records, six cases/window. Not the full CRUD/query scenario matrix or an ingestion/mutation/aggregate result.',
            'execution_order': list(full.ORDER), 'settings': full.SETTINGS,
            'metric_scope': 'Pooled measured operation p50/mean, nearest-rank p95 and finite summed-trial throughput. Ratios use unrounded samples/statistics; raw Redis controls remain separate. Lazy first-operation transport work remains included; no capacity/confidence/causal CPU claim.',
            'builds': {'baseline': a1, 'candidate': b1},
            'windows': {name: {'build': 'baseline' if name.startswith('A') else 'candidate',
                              'trial_records': 24, 'warmup_records': 6, 'measured_records': 18,
                              'summary': summaries[name]} for name in full.ORDER},
            'pooled': pooled, 'pairs': pairs,
            'target': {'requirement': 'At least 10% less pooled one-client point-read p50 independently in managed and unmanaged; candidate <= 0.9 * baseline.',
                       'required_elapsed_reduction_percent': 10., 'cases': target,
                       'met': all(case['met'] for case in target)},
            'local_audit_source_sha256': {name: full.digest(Path(__file__).with_name(name)) for name in ['screen_compare.py', 'point_read.py', 'compare.py']}}


def write_outputs(result, output):
    output.mkdir(parents=True, exist_ok=False)
    (output / 'comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    for name, rows in [('comparison.csv', [full.flat(row) for row in result['pooled']]),
                       ('pairs.csv', [{'pair': pair['name'], **full.flat(row)} for pair in result['pairs'] for row in pair['rows']])]:
        with (output / name).open('w', newline='') as file:
            writer = csv.DictWriter(file, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)
    # Keep the limited screen visibly distinct even when sharing metric tables.
    report = '# Native point-read target screen (limited selection)\n\n'
    report += '96 validated records across A1/B1/B2/A2: six point-read cases/window, 24 warmups and 72 measured records. '
    report += '**Not a full CRUD/query matrix.** Target: **' + ('MET' if result['target']['met'] else 'NOT MET') + '**.\n\n'
    report += result['metric_scope'] + '\n\n'
    for name, rows in [('Pooled', result['pooled'])] + [(pair['name'].capitalize() + ' pair (baseline→candidate)', pair['rows']) for pair in result['pairs']]:
        report += '## ' + name + '\n\n| Mode | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py |\n|---|---:|---:|---:|---:|---:|---:|---:|\n'
        for row in rows:
            a, b = row['baseline'], row['candidate']
            report += f"| {row['mode']} | {row['clients']} | {a['p50_ms']:.4f}→{b['p50_ms']:.4f} | {a['p95_ms']:.4f}→{b['p95_ms']:.4f} | {a['mean_ms']:.4f}→{b['mean_ms']:.4f} | {row['p50_elapsed_reduction_percent']:.2f}% | {a['summed_trial_operations_per_second']:.2f}→{b['summed_trial_operations_per_second']:.2f} | {row['candidate_elapsed_over_redis_py']:.3f}× |\n"
        report += '\n'
    (output / 'report.md').write_text(report)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in full.ORDER:
        parser.add_argument('--' + name.lower(), type=Path, required=True)
    parser.add_argument('--baseline-source', default='3b42245402a2d7cf6f363faf7ac713cd36621f90')
    parser.add_argument('--candidate-source')
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--require-target', action='store_true')
    args = parser.parse_args()
    paths = {name: getattr(args, name.lower()) for name in full.ORDER}
    result = compare({name: json.loads(path.read_text()) for name, path in paths.items()},
                     baseline_source=args.baseline_source, candidate_source=args.candidate_source)
    result['input_sha256'] = {name: full.digest(path) for name, path in paths.items()}
    write_outputs(result, args.out)
    print('PASS: explicit limited point-read screen, 96 records, six pooled cases, 12 pair cases; target', 'MET' if result['target']['met'] else 'NOT MET')
    if args.require_target and not result['target']['met']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
