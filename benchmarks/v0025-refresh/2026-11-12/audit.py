"""Recompute summaries and check validated repetition coverage without Redis."""
from pathlib import Path
import json
import statistics

HERE = Path(__file__).resolve().parent


def summarize(records, keys, metrics):
    result = []
    for key in sorted({tuple(r[k] for k in keys) for r in records}, key=str):
        selected = [r for r in records if tuple(r[k] for k in keys) == key
                    and not r.get('warmup', False)]
        if selected:
            row = {**dict(zip(keys, key)), 'runs': len(selected)}
            for metric in metrics:
                values = [r[metric] for r in selected]
                row[metric] = {'median': statistics.median(values),
                               'minimum': min(values), 'maximum': max(values)}
            result.append(row)
    return result


def main():
    data = json.loads((HERE / 'measurements.json').read_text())
    published = json.loads((HERE / 'summary.json').read_text())
    contracts = {
        'bulk': (['method', 'writers'], ['seconds', 'worker_rss'], 16, 4, 3, 'validated'),
        'reads': (['method', 'readers', 'batch_rows'],
                  ['seconds', 'first_batch_seconds', 'max_worker_peak_rss_bytes'],
                  84, 12, 6, 'exact_full_values_validated'),
        'union': (['input_rows', 'cardinality', 'stream'],
                  ['peak_rss_bytes', 'complete_seconds'], 36, 12, 3, 'validated'),
        'ipc': (['direction', 'workload', 'format', 'method'],
                ['seconds', 'peak_rss_bytes'], 144, 24, 5, 'validated'),
        'cross': (['engine', 'mode', 'workers'], ['seconds'],
                  72, 12, 5, 'exact_full_values_validated'),
        'throughput': (['query', 'concurrency'], ['qps', 'p95_ms'],
                       30, 10, 3, 'reference_validated'),
        'policies': (['policy', 'concurrency'],
                     ['qps', 'p95_ms', 'cold_qps', 'cold_p95_ms'], 36, 12, 3, 'validated'),
    }
    env = data['environment']
    assert (env['driver_version'], env['qpf'], env['search_workers'],
            env['routing'], env['connections']) == ('v0.0.25', 4, 6, 'mnp', 32)
    for name, (keys, metrics, count, cases, repetitions, valid) in contracts.items():
        records = data[name]
        assert len(records) == count, name
        assert all(r[valid] is True for r in records), name
        actual = summarize(records, keys, metrics)
        assert len(actual) == cases and all(r['runs'] == repetitions for r in actual), name
        assert actual == published[name], name
        print(f'{name}: {count} validated records; {cases} complete cases; summaries match')
    print('Arithmetic/coverage audit passed; this does not rerun SQL or revalidate discarded rows.')


if __name__ == '__main__':
    main()
