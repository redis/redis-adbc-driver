"""Audit the complete comparison after merged driver PRs #411–413."""
import json
from pathlib import Path
import statistics
import sys

import arrow_audit

COMMIT = 'ba855f7995b2685182ed1331bcf6703d73089c4a'
VERSION = 'v0.1.0-dev.merged-perf'


def summarize(data):
    result = arrow_audit.summarize(data, commit=COMMIT, version=VERSION)
    assert data['status'] == 'complete'
    assert data['metadata']['scope'] == f'SQL/Arrow/native comparison; driver {VERSION}'
    for suite in ('reads', 'writes', 'creation'):
        assert all(r['validated'] and not r.get('errors') for r in data[suite])
        for case in result[suite]:
            keys = ('mode', 'readers', 'batch_rows') if suite == 'reads' else ('mode', 'api', 'writers', 'operation')
            rows = [r for r in data[suite] if not r['warmup'] and all(r[k] == case[k] for k in keys)]
            assert all(r['peak_worker_rss_bytes'] > 0 for r in rows)
            case['median_peak_worker_rss_bytes'] = statistics.median(r['peak_worker_rss_bytes'] for r in rows)
    return result


def main():
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'merged-main-2026-11-13'
    actual = summarize(json.loads((directory / 'measurements.json').read_text()))
    assert actual == json.loads((directory / 'summary.json').read_text())
    print('PASS: all 156 trials (84 reads, 40 writes, 32 creation); complete validation, coverage, QPF and arithmetic')


if __name__ == '__main__':
    main()
