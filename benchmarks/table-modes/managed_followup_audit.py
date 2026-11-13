"""Audit the matched managed follow-up and separately labeled call-size controls."""
import itertools
import json
import math
from pathlib import Path
import statistics
import sys

REVISIONS = {
    'base': ('f630e117201752b6ccf8c2dcb4da1d3e17203c3a', 'v0.0.26-dev.main-perf'),
    'candidate': ('62b0f6671922f6d8e699c38957db70cac41eb6c6', 'v0.0.26-dev.managed-followup'),
}
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
QPF = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def summarize(documents):
    assert set(documents) == set(REVISIONS)
    out = {}
    for label, (commit, version) in REVISIONS.items():
        data = documents[label]
        meta = data['metadata']
        assert data['status'] == 'complete' and data['failures'] == [] and data['creation'] == []
        assert data['final_qpf'] == QPF
        assert meta['driver_source_commit'] == commit and meta['driver_version'] == version
        assert meta['source_sha256'] == SOURCE and len(meta['driver_sha256']) == 64
        assert meta['read_rows'] == 100000 and meta['write_rows'] == 10000 and meta['columns'] == 105
        assert meta['read_repetitions'] == 6 and meta['write_repetitions'] == 3
        assert meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
        assert meta['additional_arrow_call_rows'] == 10000 and meta['read_modes'] == ['managed']
        assert meta['write_paths'] == ['managed_sql', 'managed_arrow']
        assert meta['packages'] == documents['base']['metadata']['packages']
        cases = {
            'reads': [{'readers': n, 'batch_rows': b} for n, b in itertools.product((1, 4), (512, 4096))],
            'writes': ([{'api': a, 'writers': n, 'batch_rows': 1000} for a, n in itertools.product(('sql', 'arrow'), (1, 4))] +
                       [{'api': 'arrow', 'writers': n, 'batch_rows': 10000} for n in (1, 4)])}
        out[label] = {}
        for suite, keys in cases.items():
            repeats = 6 if suite == 'reads' else 3
            records = data[suite]
            assert len(records) == len(keys) * (repeats + 1)
            out[label][suite] = []
            for key in keys:
                trials = [r for r in records if all(r[k] == v for k, v in key.items())]
                assert sorted(r['repeat'] for r in trials) == list(range(-1, repeats))
                for r in trials:
                    assert r['mode'] == 'managed' and r['columns'] == 105
                    assert r['rows'] == (100000 if suite == 'reads' else 10000)
                    assert r['warmup'] == (r['repeat'] == -1)
                    assert r['validated'] and math.isfinite(r['seconds']) and r['seconds'] > 0 and not r.get('errors')
                    if suite == 'reads':
                        assert r['tasks'] == (1 if r['readers'] == 1 else 8)
                        assert 0 < r['first_batch_seconds'] < r['seconds']
                    else:
                        assert r['status'] == 'validated' and r['operation'] == 'append'
                        assert r['index_strategy'] == 'full_field_fixture' and not r['setup_in_clock']
                times = [r['seconds'] for r in trials if not r['warmup']]
                out[label][suite].append({**key, 'measured_repetitions': repeats,
                                         'median_seconds': statistics.median(times),
                                         'min_seconds': min(times), 'max_seconds': max(times)})
    return out


def validate_diagnostics(data):
    assert set(data) == {'synthetic', 'integer_conversion'}
    assert set(data['synthetic']) == set(data['integer_conversion']) == set(REVISIONS)
    for label in REVISIONS:
        records = data['synthetic'][label]
        assert len(records) == 16
        for operation, size in [('write', 1000), ('write', 10000), ('read', 1), ('read', 4)]:
            trials = [r for r in records if r['operation'] == operation and r['size'] == size]
            assert sorted(r['trial'] for r in trials) == [0, 1, 2, 3]
            for r in trials:
                assert r['warmup'] == (r['trial'] == 0)
                assert math.isfinite(r['seconds']) and r['seconds'] > 0
                for counts in r['commands'].values():
                    assert counts['count'] >= 0 and math.isfinite(counts['summed_client_seconds'])
                    assert counts['summed_client_seconds'] >= 0
                if operation == 'write':
                    assert r['commands']['eval-row-batch']['count'] == 40
                    assert r['commands']['eval-index-notify']['count'] == 40
                if operation == 'read' and size == 4 and not r['warmup']:
                    assert r['commands']['ft.search']['count'] == (392 if label == 'base' else 32)
                    assert r['commands']['pipelines']['count'] == (448 if label == 'base' else 88)
        samples = data['integer_conversion'][label]
        assert len(samples) == 3
        for sample in samples:
            assert math.isfinite(sample['ns_per_10000_values']) and sample['ns_per_10000_values'] > 0
            assert sample['bytes_per_op'] == 96 and sample['allocs_per_op'] == 3


def main():
    p = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'managed-followup-2026-11-12'
    actual = summarize(json.loads((p / 'measurements.json').read_text()))
    assert actual == json.loads((p / 'summary.json').read_text())
    validate_diagnostics(json.loads((p / 'diagnostics.json').read_text()))
    print('PASS: 56 reads, 48 writes, 32 diagnostic trials and 6 microbench samples; coverage and arithmetic')


if __name__ == '__main__':
    main()
