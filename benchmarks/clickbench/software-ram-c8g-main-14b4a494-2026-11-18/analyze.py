"""Produce full-query comparisons, requiring independent hot-trial correctness."""
import argparse
import csv
import hashlib
import json
import math
from pathlib import Path

HERE = Path(__file__).resolve().parent


def hot(values):
    pair = values[1:3]
    return min(pair) if len(pair) == 2 and all(
        isinstance(v, (float, int)) and not isinstance(v, bool) and
        math.isfinite(v) and v >= 0 for v in pair) else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', type=Path)
    parser.add_argument('--correctness', type=Path)
    args = parser.parse_args()
    manifest = json.loads((HERE / 'published/manifest.json').read_text())
    published = []
    for source in manifest:
        file = HERE / 'published' / (source['entry'] + '.json')
        assert hashlib.sha256(file.read_bytes()).hexdigest() == source['sha256']
        data = json.loads(file.read_text())
        assert data['machine'] == 'c8g.metal-48xl'
        assert len(data['result']) == 43 and all(len(r) == 3 for r in data['result'])
        complete = data.get('cluster_size', 1) == 1 and data.get('hardware', 'cpu') == 'cpu' and all(
            hot(row) is not None for row in data['result'])
        assert complete == source['complete_single_host_cpu_hot_series']
        published.append((source, data))
    print(f'Verified {len(published)} pinned source hashes; '
          f'{sum(s["complete_single_host_cpu_hot_series"] for s, _ in published)} complete single-host CPU series')
    if not args.run:
        return
    if not args.correctness:
        parser.error('--correctness is required with --run')
    run = json.loads(args.run.read_text())
    checks = json.loads(args.correctness.read_text())['checks']
    assert run['metadata']['loaded_rows'] == 99_997_497 and len(run['result']) == 43
    assert len(run['queries']) == 43
    expected_trials = {(n, t) for n in range(1, 44) for t in range(1, 4)}
    assert len(run['trials']) == 129 and {(v['query'], v['trial']) for v in run['trials']} == expected_trials
    assert len(checks) == 129 and {(v['query'], v['trial']) for v in checks} == expected_trials
    assert run['metadata']['driver_version'] == 'v0.0.29+main.14b4a494'
    assert run['metadata']['driver_source_commit'] == '14b4a4940e78bf5496a0532caf6b8e800d38dd2a'
    assert run['metadata']['driver_sha256'] == run['metadata']['driver_sha256_after'] == '1cc9a55002f2a1a77a40a7c6eaa4ad6b75d22f14028d0eadbf7b026f4e4426c4'
    assert run['metadata']['table_mode'] == 'unmanaged' and run['metadata']['storage'] == 'ram'
    canonical = json.loads((HERE / 'canonical-source-verification.json').read_text())
    query_bytes = ('\n'.join(run['queries']) + '\n').encode()
    assert hashlib.sha256(query_bytes).hexdigest() == canonical['queries']['sha256']
    rawdir = args.run.parent / (args.run.stem + '-raw')
    raw_records = {}
    for check in checks:
        raw = rawdir / f"q{check['query']:02}-{check['trial']}.json"
        assert hashlib.sha256(raw.read_bytes()).hexdigest() == check['raw_sha256']
        raw_records[(check['query'], check['trial'])] = json.loads(raw.read_text())
    verdict = {(c['query'], c['trial']): c['status'] for c in checks}
    redis_hot = []
    for number, timings in enumerate(run['result'], 1):
        validated = all(verdict.get((number, t)) == 'valid' for t in (2, 3))
        redis_hot.append(hot(timings) if validated else None)
    full = all(v is not None for v in redis_hot)
    summaries = []
    for source, data in published:
        single_cpu = data.get('cluster_size', 1) == 1 and data.get('hardware', 'cpu') == 'cpu'
        baseline_hot = [hot(row) for row in data['result']]
        pairs = [(a, b) for a, b in zip(redis_hot, baseline_hot) if a is not None and b is not None]
        complete = full and source['complete_single_host_cpu_hot_series']
        summaries.append({'entry': source['entry'], 'system': source['system'], 'date': source['date'],
                          'same_machine_type': True, 'single_host_cpu': single_cpu,
                          'paired_valid_hot_queries': len(pairs) if single_cpu else 0,
                          'full_43_query_ratio': math.exp(sum(math.log((a + .01) / (b + .01))
                              for a, b in pairs) / 43) if complete else None,
                          'queries_redis_faster': sum(a < b for a, b in pairs) if single_cpu else None,
                          'source_url': source['url']})
    summary = {'complete_validated_redis_hot_queries': sum(v is not None for v in redis_hot),
               'redis_trial_correctness': run['metadata'].get('correctness'),
               'overall_comparison_available': full,
               'formula': 'exp(mean(log((redis_hot_seconds + 0.01)/(published_hot_seconds + 0.01)))) across all 43 queries',
               'ratio_direction': 'greater than one means Redis took longer',
               'missing_policy': 'null; no imputed time and no overall score from a partial query subset',
               'comparisons': summaries}
    (HERE / 'comparison.json').write_text(json.dumps(summary, indent=2) + '\n')
    with (HERE / 'all-query-hot-seconds.csv').open('w', newline='') as file:
        writer = csv.writer(file)
        writer.writerow(['query', 'redis_hot_seconds', 'redis_hot_status'] + [s['entry'] for s, _ in published])
        for n, value in enumerate(redis_hot, 1):
            writer.writerow([n, value, 'valid' if value is not None else 'unavailable'] +
                            [hot(data['result'][n - 1]) for _, data in published])
    with (HERE / 'query-summary.csv').open('w', newline='') as file:
        writer = csv.writer(file)
        columns = ['query', 'sql']
        for trial in range(1, 4):
            columns += [f'trial{trial}_seconds', f'trial{trial}_correctness',
                        f'trial{trial}_failure', f'trial{trial}_last_stage']
        writer.writerow(columns + ['hot_seconds', 'hot_status'])
        for number, sql in enumerate(run['queries'], 1):
            row = [number, sql]
            for trial in range(1, 4):
                raw = raw_records[(number, trial)]
                stage = raw.get('partial_progress', {}).get('active_stage', '')
                failure = raw.get('failure_summary') or raw.get('error_type', '')
                row += [run['result'][number - 1][trial - 1], verdict[(number, trial)], failure, stage]
            value = redis_hot[number - 1]
            writer.writerow(row + [value, 'valid' if value is not None else 'unavailable'])
    with (HERE / 'per-query-comparisons.csv').open('w', newline='') as file:
        writer = csv.writer(file)
        writer.writerow(['query', 'entry', 'system', 'published_date', 'single_host_cpu',
                         'redis_hot_seconds', 'published_hot_seconds', 'redis_over_published',
                         'redis_over_published_with_0.01_offset', 'status'])
        for source, data in published:
            eligible = data.get('cluster_size', 1) == 1 and data.get('hardware', 'cpu') == 'cpu'
            for n, (a, row) in enumerate(zip(redis_hot, data['result']), 1):
                b = hot(row)
                paired = eligible and a is not None and b is not None
                status = 'paired_valid_hot' if paired else 'redis_unavailable' if a is None else 'published_unavailable'
                if not eligible:
                    status = 'different_topology'
                writer.writerow([n, source['entry'], source['system'], source['date'], eligible,
                                 a, b, a / b if paired and b != 0 else None,
                                 (a + .01) / (b + .01) if paired else None, status])
    print(f'Compared {sum(v is not None for v in redis_hot)}/43 independently validated Redis hot results; '
          f'overall comparison {"available" if full else "withheld"}')


if __name__ == '__main__':
    main()
