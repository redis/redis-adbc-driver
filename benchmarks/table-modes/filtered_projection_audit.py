"""Audit the complete filtered unmanaged projection comparison."""
import itertools
import json
from pathlib import Path
import statistics
import sys

SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
BUILDS = {
    'baseline': ('5f759300c12f5191a36d2f2b5a29555ef14ef486', 'v0.1.0-dev.merged-perf-2'),
    'candidate': ('783711f185e1a6a63ae9365e447b8fb7a3ec981b', 'v0.1.0-dev.filtered-arrow'),
}
QPF = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def summarize(data):
    assert data['status'] == 'complete' and data['source_sha256'] == SOURCE
    environment = data['environment']
    assert environment['software_version'] == '8.2.6' and environment['redis_version'] == '8.8.0'
    assert environment['search_module'] == 80808 and environment['cpus'] == 4
    assert environment['container_memory_bytes'] == 8589934592
    assert data['tls_certificate_verified'] and data['toolchain'] == 'go1.27.3'
    summary = {}
    for variant, (commit, version) in BUILDS.items():
        run = data[variant]
        meta = run['metadata']
        assert meta['driver_source_commit'] == commit and meta['driver_version'] == version
        assert meta['source_sha256'] == SOURCE and meta['automatic_retries'] == 0
        assert meta['driver_sha256'] == run['build']['driver_sha256']
        assert run['build']['source'] == commit and run['build']['version'] == version
        assert meta['protocol'] == 3 and meta['tls'] and meta['read_repetitions'] == 6
        assert meta['read_rows'] == 100000 and meta['columns'] == 105
        assert run['status'] == 'complete' and run['final_qpf'] == QPF
        assert run['writes'] == [] and run['failures'] == [] and len(run['reads']) == 28
        summary[variant] = []
        for readers, batch in itertools.product((1, 4), (512, 4096)):
            rows = [r for r in run['reads'] if r['readers'] == readers and r['batch_rows'] == batch]
            assert sorted(r['repeat'] for r in rows) == list(range(-1, 6))
            for row in rows:
                assert row['warmup'] == (row['repeat'] == -1) and row['validated']
                assert row['mode'] == 'unmanaged' and row['rows'] == 100000 and row['columns'] == 105
                assert row['tasks'] == (1 if readers == 1 else 8)
                assert row['strategy'] == ('full_LOAD' if readers == 1 else 'live_residual_HGETALL')
                assert row['peak_worker_rss_bytes'] > 0 and 0 < row['first_batch_seconds'] < row['seconds']
            measured = [r for r in rows if not r['warmup']]
            summary[variant].append({
                'readers': readers, 'batch_rows': batch, 'measured_repetitions': len(measured),
                'median_seconds': statistics.median(r['seconds'] for r in measured),
                'min_seconds': min(r['seconds'] for r in measured),
                'max_seconds': max(r['seconds'] for r in measured),
                'median_first_batch_seconds': statistics.median(r['first_batch_seconds'] for r in measured),
                'median_peak_worker_rss_bytes': statistics.median(r['peak_worker_rss_bytes'] for r in measured),
                'min_peak_worker_rss_bytes': min(r['peak_worker_rss_bytes'] for r in measured),
                'max_peak_worker_rss_bytes': max(r['peak_worker_rss_bytes'] for r in measured),
            })
    assert data['baseline']['metadata']['packages'] == data['candidate']['metadata']['packages']
    return summary


def main():
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / 'docs/sdlc/changes/unmanaged-filtered-arrow/benchmark.json'
    data = json.loads(path.read_text())
    assert summarize(data) == data['summary']
    print('PASS: all 56 filtered projection trials; exact builds/input, complete validation, QPF and timing/RSS arithmetic')


if __name__ == '__main__':
    main()
