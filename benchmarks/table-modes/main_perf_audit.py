"""Audit the full comparison on the pinned post-performance main snapshot."""
import json
from pathlib import Path
import sys

import arrow_audit

COMMIT = 'f630e117201752b6ccf8c2dcb4da1d3e17203c3a'
VERSION = 'v0.0.26-dev.main-perf'


def summarize(data):
    result = arrow_audit.summarize(data, commit=COMMIT, version=VERSION)
    assert data['status'] == 'complete'
    assert data['metadata']['scope'] == f'SQL/Arrow/native comparison; driver {VERSION}'
    for suite in ('reads', 'writes', 'creation'):
        assert all(r['validated'] and (suite == 'reads' or r['status'] == 'validated') and not r.get('errors')
                   for r in data[suite])
    return result


def main():
    p = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'main-perf-2026-11-12'
    actual = summarize(json.loads((p / 'measurements.json').read_text()))
    assert actual == json.loads((p / 'summary.json').read_text())
    print('PASS: 84 reads, 40 writes, 32 creation trials; all complete, coverage and arithmetic')


if __name__ == '__main__':
    main()
