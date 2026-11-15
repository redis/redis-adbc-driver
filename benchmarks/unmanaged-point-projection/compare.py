"""Offline complete 32-case native width ABBA comparison; no Redis calls."""
import argparse
import csv
import importlib.util
import json
import math
from pathlib import Path

HERE = Path(__file__).resolve().parent

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


audit = load('_unmanaged_width_audit', HERE / 'audit.py')
full = load('_crud_width_metrics', HERE.parent / 'crud-execution/compare.py')
ORDER = ('A1', 'B1', 'B2', 'A2')
BUILD_KEYS = {'driver_source_commit', 'driver_sha256', 'driver_version', 'cli_sha256'}
CASE_KEYS = ('width', 'mode', 'projection_columns', 'clients')
FIXED_METADATA = {
    'automatic_retries': 0, 'protocol': 3, 'tls': True, 'repetitions': 3,
    'operations': 32, 'point_rows': 1000, 'columns': 4,
    'suite': 'unmanaged_known_key_projection',
    'generator': 'id=1..N; grp=id%16; amount=9007199254740993+id; label=row-{id:08d} + NUL + label',
    'timing_scope': 'Persistent client/cursor objects; SQL execute plus full Arrow fetch or HMGET plus equivalent typed Arrow conversion. Lazy first transport work included. Setup/teardown and independent validation excluded.',
    'addressing': full.FIXED_METADATA['addressing'],
    'widths': list(audit.WIDTHS), 'projection_columns': [1, 4],
    'clients': [1, 4], 'modes': list(audit.MODES), 'payload_bytes': 65536,
    'byte_scope': 'Value-byte oracles only; no measured packet/wire-byte attribution.',
}
BYTE_SCOPE = 'Exact source value bytes for requested keys, excluding field names/RESP/TLS. Not measured wire bytes.'


def validate(data):
    meta = data['metadata']
    assert set(meta) == set(FIXED_METADATA) | BUILD_KEYS | {'harness_sha256', 'helper_sha256', 'packages'}
    for key, value in FIXED_METADATA.items():
        assert type(meta[key]) is type(value) and meta[key] == value, key
    for key, length in [('driver_source_commit', 40), ('driver_sha256', 64), ('cli_sha256', 64), ('harness_sha256', 64)]:
        assert len(meta[key]) == length and all(c in '0123456789abcdef' for c in meta[key]), key
    assert meta['driver_version'].startswith('v') and len(meta['driver_version']) > 1
    assert set(meta['helper_sha256']) == {'scenarios.py', 'table-modes/run.py'}
    assert all(len(value) == 64 and all(c in '0123456789abcdef' for c in value) for value in meta['helper_sha256'].values())
    assert set(meta['packages']) == {'pyarrow', 'adbc-driver-manager', 'redis', 'hiredis'}
    assert all(isinstance(value, str) and value for value in meta['packages'].values())
    summary = audit.summarize(data)
    assert len(data['trials']) == 128 and len(summary) == 32
    assert type(data['failures']) is list and data['failures'] == []
    for row in data['trials']:
        assert row['validated'] is True and type(row['warmup']) is bool
        assert type(row['seconds']) in (int, float) and math.isfinite(row['seconds'])
        assert all(type(row[key]) is int for key in ['projection_columns', 'clients', 'repeat', 'operations', 'fixture_rows'])
        assert row['byte_oracle']['scope'] == BYTE_SCOPE
    assert all(row['source_preserved'] is True for row in data['fixtures'])
    return summary


def grouped(windows):
    groups = {}
    for window in windows:
        for row in window['trials']:
            if not row['warmup']:
                key = tuple(row[name] for name in CASE_KEYS)
                groups.setdefault(key, []).append(row)
    return groups


def compare_groups(baseline, candidate):
    assert set(baseline) == set(candidate) and len(baseline) == 32
    a = {key: full.statistics_of(rows) for key, rows in baseline.items()}
    b = {key: full.statistics_of(rows) for key, rows in candidate.items()}
    rows = []
    for key in sorted(a):
        width, mode, columns, clients = key
        raw = (width, 'redis_py', columns, clients)
        before, after = a[key], b[key]
        oracle = baseline[key][0]['byte_oracle']
        assert all(trial['byte_oracle'] == oracle for trial in baseline[key] + candidate[key])
        rows.append(dict(zip(CASE_KEYS, key)) | {
            'baseline': before, 'candidate': after,
            'p50_baseline_over_candidate': before['p50_ms'] / after['p50_ms'],
            'p50_elapsed_reduction_percent': (1 - after['p50_ms'] / before['p50_ms']) * 100,
            'p95_elapsed_change_percent': (after['p95_ms'] / before['p95_ms'] - 1) * 100,
            'mean_elapsed_change_percent': (after['mean_ms'] / before['mean_ms'] - 1) * 100,
            'summed_trial_throughput_ratio': after['summed_trial_operations_per_second'] / before['summed_trial_operations_per_second'],
            'baseline_redis_py_p50_ms': a[raw]['p50_ms'], 'candidate_redis_py_p50_ms': b[raw]['p50_ms'],
            'baseline_elapsed_over_redis_py': before['p50_ms'] / a[raw]['p50_ms'],
            'candidate_elapsed_over_redis_py': after['p50_ms'] / b[raw]['p50_ms'],
            'value_byte_oracle_per_trial': oracle,
        })
    return rows


def compare(windows, *, baseline_source=None, candidate_source=None, harness_sha256=None):
    assert set(windows) == set(ORDER)
    summaries = {name: validate(windows[name]) for name in ORDER}
    a1, b1, b2, a2 = (windows[name]['metadata'] for name in ORDER)
    assert a1 == a2 and b1 == b2, 'Build/measurement identity changed within its two windows'
    assert {key: value for key, value in a1.items() if key not in BUILD_KEYS} == {
        key: value for key, value in b1.items() if key not in BUILD_KEYS}, 'Builds differ outside source/library/version/CLI'
    assert a1['driver_source_commit'] != b1['driver_source_commit'] and a1['driver_sha256'] != b1['driver_sha256']
    if baseline_source is not None:
        assert a1['driver_source_commit'] == baseline_source
    if candidate_source is not None:
        assert b1['driver_source_commit'] == candidate_source
    if harness_sha256 is not None:
        assert a1['harness_sha256'] == harness_sha256
    pooled = compare_groups(grouped([windows['A1'], windows['A2']]), grouped([windows['B1'], windows['B2']]))
    pairs = []
    for name, base, candidate, order in [('forward', 'A1', 'B1', ['baseline', 'candidate']),
                                       ('reverse', 'A2', 'B2', ['candidate', 'baseline'])]:
        pairs.append({'name': name, 'baseline_window': base, 'candidate_window': candidate,
                      'execution_order': order,
                      'rows': compare_groups(grouped([windows[base]]), grouped([windows[candidate]]))})
    return {
        'schema_version': 1, 'execution_order': list(ORDER),
        'scope': '512 complete native known-key width trial records: 128 warmups and 384 measured trials. Four payload layouts, one/four selected columns, one/four clients, unmanaged/redis-py. Not the standard CRUD/query matrix.',
        'metric_scope': 'P50/means pool measured operation latencies; p95 uses nearest rank. Ratios use unrounded samples/statistics. All raw Redis controls and both complete pairs are retained without causal normalization.',
        'throughput_scope': 'Summed-trial throughput divides all measured operations by summed complete trial wall time; median-trial throughput is the median of per-trial rates. Finite closed-loop throughput includes coordination/scheduling, not saturated capacity. Means are not the inverse of concurrent workload completion rate.',
        'value_byte_scope': 'Oracles independently describe exact selected/full-HASH source value bytes, not measured wire bytes, server CPU, allocation or causal latency attribution.',
        'limits': ['No NULL/ACL latency cases are timed here; all-selected-NULL and HGETALL-only principals can incur an extra read/fallback, including full payload transfer.',
                   'The same index definition is used, but volatile FT.INFO statistics/reply lengths can differ.',
                   'Independent build/image/runtime/server/helper freeze and actual chronological window records must accompany JSON metadata.',
                   'The measured harness hash is preserved even if a later comment-only edit changes the local harness hash.',
                   'Finite samples do not establish confidence intervals, production capacity or a general point-read/CRUD gain.'],
        'settings': full.SETTINGS, 'builds': {'baseline': a1, 'candidate': b1},
        'windows': {name: {'build': 'baseline' if name.startswith('A') else 'candidate',
                           'trial_records': 128, 'warmup_records': 32, 'measured_records': 96,
                           'summary': summaries[name], 'source_fixtures': windows[name]['fixtures']} for name in ORDER},
        'pooled': pooled, 'pairs': pairs,
        'local_audit_source_sha256': {name: full.digest(HERE / name) for name in ['compare.py', 'audit.py', 'run.py']},
    }


def flat(row):
    result = {key: row[key] for key in CASE_KEYS}
    for build in ['baseline', 'candidate']:
        result.update({build + '_' + key: value for key, value in row[build].items()})
    result.update({key: value for key, value in row.items() if key not in CASE_KEYS and key not in ['baseline', 'candidate', 'value_byte_oracle_per_trial']})
    result.update({'oracle_' + key: value for key, value in row['value_byte_oracle_per_trial'].items() if key != 'scope'})
    return result


def write_outputs(result, output):
    output.mkdir(parents=True, exist_ok=False)
    (output / 'comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    for name, rows in [('comparison.csv', [flat(row) for row in result['pooled']]),
                       ('pairs.csv', [{'pair': pair['name'], **flat(row)} for pair in result['pairs'] for row in pair['rows']])]:
        with (output / name).open('w', newline='') as file:
            writer = csv.DictWriter(file, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)
    text = '# Native unmanaged point-projection width ABBA comparison\n\n'
    text += result['scope'] + '\n\n' + result['metric_scope'] + '\n\n' + result['throughput_scope'] + '\n\n' + result['value_byte_scope'] + '\n\n'
    for title, rows in [('Pooled measured samples', result['pooled'])] + [(pair['name'].capitalize() + ' pair (baseline→candidate)', pair['rows']) for pair in result['pairs']]:
        text += '## ' + title + '\n\n| Width | Mode | Columns | Clients | P50 ms B→C | P95 ms B→C | Mean ms B→C | P50 less elapsed | Finite ops/s B→C | C / redis-py |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n'
        for row in rows:
            a, b = row['baseline'], row['candidate']
            text += f"| {row['width']} | {row['mode']} | {row['projection_columns']} | {row['clients']} | {a['p50_ms']:.4f}→{b['p50_ms']:.4f} | {a['p95_ms']:.4f}→{b['p95_ms']:.4f} | {a['mean_ms']:.4f}→{b['mean_ms']:.4f} | {row['p50_elapsed_reduction_percent']:.2f}% | {a['summed_trial_operations_per_second']:.2f}→{b['summed_trial_operations_per_second']:.2f} | {row['candidate_elapsed_over_redis_py']:.3f}× |\n"
        text += '\n'
    text += '## Limits\n\n' + '\n'.join('- ' + item for item in result['limits']) + '\n'
    (output / 'report.md').write_text(text)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ORDER:
        parser.add_argument('--' + name.lower(), type=Path, required=True)
    parser.add_argument('--baseline-source', default='3b42245402a2d7cf6f363faf7ac713cd36621f90')
    parser.add_argument('--candidate-source', default='a944c99c500f2f5904ff669bea5cb9fb9b6b8e39')
    parser.add_argument('--harness-sha256')
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    paths = {name: getattr(args, name.lower()) for name in ORDER}
    result = compare({name: json.loads(path.read_text()) for name, path in paths.items()},
                     baseline_source=args.baseline_source, candidate_source=args.candidate_source,
                     harness_sha256=args.harness_sha256)
    result['input_sha256'] = {name: full.digest(path) for name, path in paths.items()}
    write_outputs(result, args.out)
    print('PASS: 512 native width records, 32 pooled cases, 64 pair cases; source/value-byte oracles and all Redis controls retained')


if __name__ == '__main__':
    main()
