#!/usr/bin/env python3
"""Recompute the complete retained C-ABI/native evidence, without executing Redis."""
import hashlib
import itertools
import json
from pathlib import Path
import statistics

HERE = Path(__file__).resolve().parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    data = json.loads((HERE / '2026-11-13/results.json').read_text())
    assert data['decision'] == 'evidence_only_default_unchanged'
    assert data['base_commit'] == '24f654b76b98f3d8960af94a3ca0230185d467a1'
    assert data['fixture_sha256'] == sha(HERE / 'probe.go.txt')
    assert data['runner_sha256'] == sha(HERE / 'run.py')
    assert data['patch_sha256'] == sha(HERE / 'patches/omit-forced-gc.patch')
    go = HERE.parents[1] / 'go'
    assert data['module_sha256'] == sha(go / 'go.mod')
    assert data['sum_sha256'] == sha(go / 'go.sum')
    assert data['source_sha256'] == sha(go / 'pkg/driver.go')
    candidate = (go / 'pkg/driver.go').read_text().replace('runtime.GC()', '// Experimental attribution only: forced GC omitted.')
    assert hashlib.sha256(candidate.encode()).hexdigest() == data['candidate_driver_sha256']
    assert (go / 'pkg/driver.go').read_text().count('runtime.GC()') == 4
    for variant, expected_gc in [('baseline', 128), ('candidate', 0)]:
        probe = data['probe'][variant]
        assert len(probe['checks']) == len(set(probe['checks'])) == 7
        assert probe['checked_allocator_bytes'] == 0
        assert probe['library_sha256'] == data['probe_builds'][variant]
        samples = probe['samples']
        expected = set(itertools.product(['database', 'connection', 'statement', 'stream'], range(7)))
        assert len(samples) == 28 and {(s['case'], s['sample']) for s in samples} == expected
        for sample in samples:
            assert sample['warmup'] == (sample['sample'] == 0)
            assert sample['iterations'] == 128 and sample['forced_gc_delta'] == expected_gc
            assert sample['elapsed_ns'] > 0
            assert sample['ns_per_release'] == sample['elapsed_ns'] / 128
        for case, median in probe['summary'].items():
            assert median == statistics.median(s['ns_per_release'] for s in samples if s['case'] == case and not s['warmup'])
    race = [x for x in data['failed_attempts'] if x['stage'] == 'checkptr-before-ownership']
    assert len(race) == 2 and all(x['exit_code'] == -6 for x in race)
    assert len(data['failed_attempts']) == 6
    native = data.get('native')
    if native:
        for variant in ('baseline', 'candidate'):
            rows = native[variant]['reads']
            expected = set(itertools.product([1, 4], [512, 4096], range(-1, 6)))
            assert len(rows) == 28 and {(r['readers'], r['batch_rows'], r['repeat']) for r in rows} == expected
            for row in rows:
                assert row['validated'] and row['rows'] == 100000 and row['columns'] == 105
                assert row['warmup'] == (row['repeat'] == -1)
                assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
            assert native[variant]['status'] == 'complete' and not native[variant]['failures']
        assert data['status'] == 'complete'
        freeze = data['native_freeze']
        assert freeze['base_commit'] == data['base_commit']
        assert freeze['candidate_commit'] == 'b519284bcedd9ad24c48fbc53219b818dde3f68e'
        assert freeze['candidate_clean'] and not freeze['probe_present']
        assert freeze['candidate_diff'] == 'go/pkg/driver.go'
        assert freeze['candidate_driver_sha256'] == data['candidate_driver_sha256']
        assert freeze['module_sha256'] == data['module_sha256']
        assert freeze['sum_sha256'] == data['sum_sha256']
        assert data['loan_return'] == dict(empty=True, no_indexes=True, clients_closed=True)
        for variant in ('baseline', 'candidate'):
            meta = native[variant]['metadata']
            build = data['native_builds'][variant]
            assert meta['driver_source_commit'] == build['commit']
            assert meta['driver_sha256'] == build['driver_sha256']
            assert meta['driver_version'] == build['version']
            assert meta['source_sha256'] == freeze['source_sha256']
            assert meta['read_repetitions'] == 6 and meta['automatic_retries'] == 0
            assert meta['protocol'] == 3 and meta['tls']
            assert native[variant]['final_qpf'] == dict(factor=4, workers=6, routing='mnp', connections=32, memory_bytes=4294967296)
            for settings in build['go_build_settings'].values():
                assert 'vcs.modified=false' in settings
                assert 'vcs.revision=' + build['commit'] in settings
        for summary in data['native_summary']:
            for variant in ('baseline', 'candidate'):
                measured = [r for r in native[variant]['reads'] if not r['warmup'] and
                            (r['readers'], r['batch_rows']) == (summary['readers'], summary['batch_rows'])]
                seconds = [r['seconds'] for r in measured]
                rss = [r['peak_worker_rss_bytes'] for r in measured]
                assert summary[variant] == dict(samples=6, median_seconds=statistics.median(seconds),
                    min_seconds=min(seconds), max_seconds=max(seconds),
                    median_peak_worker_rss_bytes=statistics.median(rss),
                    min_peak_worker_rss_bytes=min(rss), max_peak_worker_rss_bytes=max(rss),
                    median_first_batch_seconds=statistics.median(r['first_batch_seconds'] for r in measured))
            assert summary['elapsed_percent_change'] == 100 * (summary['candidate']['median_seconds'] / summary['baseline']['median_seconds'] - 1)
            assert summary['rss_percent_change'] == 100 * (summary['candidate']['median_peak_worker_rss_bytes'] / summary['baseline']['median_peak_worker_rss_bytes'] - 1)
            assert summary['elapsed_percent_change'] > 0
        assert len(data['native_summary']) == 4
        assert all(command['exit_code'] == 0 for command in data['native_commands'].values())
    print(f"PASS: all 56 C-ABI observations, both blocked race gates, source/module/patch provenance; native {'56 complete' if native else 'pending'}")


if __name__ == '__main__':
    main()
