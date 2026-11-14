"""Audit complete fetch-page sizing evidence without Redis or native libraries."""
from collections import Counter
import hashlib
import json
from pathlib import Path
import statistics


def distributions(rows):
    result = []
    for width, mode, readers, batch in sorted({
            (r['columns'], r['mode'], r['readers'], r['batch_rows']) for r in rows}):
        selected = [r for r in rows if not r['warmup'] and
                    (r['columns'], r['mode'], r['readers'], r['batch_rows']) ==
                    (width, mode, readers, batch)]
        row = dict(columns=width, mode=mode, readers=readers, batch_rows=batch)
        for metric in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
            values = [r[metric] for r in selected]
            row[metric] = dict(samples=values, median=statistics.median(values),
                               minimum=min(values), maximum=max(values))
        result.append(row)
    return result


def audit():
    root = Path(__file__).resolve().parents[2]
    folder = root/'benchmarks/table-modes/fetch-sizing-2026-11-13'
    data = json.loads((folder/'native-results.json').read_text())
    summary = json.loads((folder/'summary.json').read_text())
    provenance = json.loads((folder/'provenance.json').read_text())
    cases = {(width, mode, readers, batch, rep)
             for width in (8, 105)
             for mode in (('managed',) if width == 8 else ('managed', 'unmanaged'))
             for readers in (1, 4) for batch in (512, 4096) for rep in range(-1, 6)}
    assert set(data) == {'baseline', 'candidate'}
    for variant, source, version in [
            ('baseline', '88774f10cf16fc4260e6ad5077097f4de8828406', 'v0.1.0-dev.allocation-base'),
            ('candidate', 'faa05f96158f92c7cb2c18f69addfb4a07202825', 'v0.1.0-dev.fetch-2048')]:
        run = data[variant]
        assert run['status'] == 'complete' and run['failures'] == []
        rows = run['reads']
        assert len(rows) == 84 and run['writes'] == [] and run['creation'] == []
        assert Counter((r['columns'], r['mode'], r['readers'], r['batch_rows'], r['repeat'])
                       for r in rows) == Counter({case: 1 for case in cases})
        for row in rows:
            assert row['validated'] is True and row['rows'] == 100000
            assert row['warmup'] is (row['repeat'] == -1)
            assert row['tasks'] == (1 if row['readers'] == 1 else 8)
            assert 0 <= row['first_batch_seconds'] <= row['seconds']
            assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
        meta = run['metadata']
        assert meta['driver_source_commit'] == source and meta['driver_version'] == version
        assert meta['source_sha256'] == 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
        assert meta['protocol'] == 3 and meta['tls'] is True
        assert meta['read_repetitions'] == 6
        assert run['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                    'connections': 32, 'memory_bytes': 4294967296}
        assert summary[variant] == distributions(rows)
        assert provenance[variant]['source'] == source
        assert provenance[variant]['driver_sha256'] == meta['driver_sha256']
    for name, digest in json.loads((folder/'sha256.json').read_text()).items():
        assert hashlib.sha256((folder/name).read_bytes()).hexdigest() == digest
    print('PASS: all168 trials/all12 cells, exact coverage/source/settings and elapsed/first-batch/RSS distributions')


if __name__ == '__main__':
    audit()
