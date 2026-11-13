"""Audit retained native and diagnostic compaction-control measurements."""
import json
from pathlib import Path
import statistics
import sys

BASELINE = '5f759300c12f5191a36d2f2b5a29555ef14ef486'
CANDIDATE = '86a27f2ba772cde03765cefa1d963edbc24c3490'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'


def distribution(values):
    return {'median': statistics.median(values), 'min': min(values), 'max': max(values)}


def summarize(data):
    metadata = data['metadata']
    assert metadata['source_sha256'] == SOURCE
    assert metadata['environment']['qpf'] == 4
    assert metadata['environment']['database_bytes'] == 4294967296
    result = {'native': {}, 'publication': {}, 'dense': {}}
    for label, commit in [('baseline', BASELINE), ('candidate', CANDIDATE)]:
        build = metadata['builds'][label]
        assert build['commit'] == commit and build['vcs_modified'] is False
        assert build['go_toolchain'] == 'go1.27.3'
        native = data['native'][label]
        assert native['status'] == 'complete' and not native['failures']
        assert not native['reads'] and not native['creation']
        meta = native['metadata']
        assert meta['driver_source_commit'] == commit
        assert meta['driver_version'] == build['version']
        assert meta['driver_sha256'] == build['driver_sha256']
        assert meta['source_sha256'] == SOURCE
        assert meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
        assert meta['write_rows'] == 10000 and meta['columns'] == 105
        assert meta['write_repetitions'] == 3 and meta['sql_batch_rows'] == 1000
        assert native['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                       'connections': 32, 'memory_bytes': 4294967296}
        assert len(native['writes']) == 8
        result['native'][label] = []
        for writers in (1, 4):
            rows = [r for r in native['writes'] if r['writers'] == writers]
            assert sorted(r['repeat'] for r in rows) == [-1, 0, 1, 2]
            for row in rows:
                assert row['mode'] == 'managed' and row['api'] == 'arrow' and row['operation'] == 'append'
                assert row['warmup'] == (row['repeat'] < 0)
                assert row['validated'] and row['status'] == 'validated' and not row.get('errors')
                assert row['rows'] == 10000 and row['columns'] == 105 and row['batch_rows'] == 1000
                assert row['setup_in_clock'] is False and row['index_strategy'] == 'full_field_fixture'
                assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
            measured = [r for r in rows if not r['warmup']]
            result['native'][label].append({'writers': writers,
                'seconds': distribution([r['seconds'] for r in measured]),
                'peak_worker_rss_bytes': distribution([r['peak_worker_rss_bytes'] for r in measured])})
        publication = data['diagnostics'][label]['publication']
        assert len(publication) == 6
        result['publication'][label] = []
        for call_rows in (1000, 10000):
            rows = [r for r in publication if r['call_rows'] == call_rows]
            assert sorted(r['trial'] for r in rows) == [0, 1, 2]
            assert all(r['validated'] and r['caller_retries'] == 0 and r['rows'] == 10000 and r['columns'] == 105 for r in rows)
            item = {'call_rows': call_rows, 'milliseconds': distribution([r['milliseconds'] for r in rows])}
            if call_rows == 1000:
                assert all(len(r['call_milliseconds']) == 10 for r in rows)
                item['ninth_call_milliseconds'] = distribution([r['call_milliseconds'][8] for r in rows])
            result['publication'][label].append(item)
        dense = data['diagnostics'][label]['dense']
        assert len(dense) == 6
        result['dense'][label] = []
        for paced in (True, False):
            rows = [r for r in dense if r['paced'] == paced]
            assert sorted(r['repeat'] for r in rows) == [0, 1, 2]
            for row in rows:
                assert row['dense'] and row['rows'] == row['unrelated_rows'] == 20000
                phase = row['phases']['copy']
                assert phase['steps'] == 314
                rounds = phase['round_trips']
                assert rounds.get('control_acquire', {}).get('calls', 0) == (314 if label == 'baseline' else 0)
                assert rounds.get('control_compact_acquire', {}).get('calls', 0) == (314 if label == 'baseline' else 0)
                assert rounds.get('control_compact_acquire_catalog', {}).get('calls', 0) == (0 if label == 'baseline' else 314)
                assert rounds['control_release']['calls'] == 314
            result['dense'][label].append({'paced': paced,
                'elapsed_milliseconds': distribution([r['elapsed_milliseconds'] for r in rows]),
                'copy_step_milliseconds': distribution([r['phases']['copy']['step_milliseconds'] for r in rows]),
                'copy_calls': distribution([sum(v['calls'] for v in r['phases']['copy']['round_trips'].values()) for r in rows]),
                'copy_commands': distribution([sum(v['commands'] for v in r['phases']['copy']['round_trips'].values()) for r in rows])})
    return result


def main():
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / 'docs/sdlc/changes/compaction-control-roundtrips'
    data = json.loads((directory / 'results.json').read_text())
    assert summarize(data) == data['summary']
    print('PASS: all 16 native trials, 12 publication samples and 12 dense profiles; source/coverage/QPF/value-validation and arithmetic')


if __name__ == '__main__':
    main()
