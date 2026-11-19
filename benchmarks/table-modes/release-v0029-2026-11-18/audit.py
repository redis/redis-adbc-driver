"""Audit complete release measurements and regenerate slide chart arithmetic."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import chart_data


def main():
    bulk = json.loads((HERE / 'table-modes.json').read_text())
    scenarios = json.loads((HERE / 'scenarios.json').read_text())
    provenance = json.loads((HERE / 'provenance.json').read_text())
    assert [len(bulk[k]) for k in ('reads', 'writes', 'creation')] == [84, 40, 32]
    assert len(scenarios['trials']) == 168
    assert chart_data.table_summary(bulk) == json.loads((HERE / 'summary.json').read_text())
    charts = chart_data.make_charts(bulk, scenarios)
    assert charts == json.loads((HERE / 'charts.json').read_text())
    with tempfile.TemporaryDirectory() as directory:
        output = Path(directory) / 'charts'
        assert chart_data.write_outputs(charts, output) == 84
        for name in ('charts.json', 'charts.csv'):
            assert (output / name).read_bytes() == (HERE / name).read_bytes(), name
    for data in (bulk, scenarios):
        meta = data['metadata']
        assert meta['driver_version'] == provenance['version'] == 'v0.0.29'
        assert meta['driver_source_commit'] == provenance['source_commit'] == '30a9eefbf13d19b3e3129171f8ecdc5579b852a2'
        assert meta['driver_sha256'] == provenance['sha256']['library']
    for name in ('run.py', 'arrow_compare.py', 'scenarios.py'):
        assert hashlib.sha256((HERE.parent / name).read_bytes()).hexdigest() == provenance['sha256'][name]
    assert provenance['release_archives_verified'] is True
    assert provenance['server']['qpf'] == 4
    assert provenance['server']['search_workers'] == 6
    assert provenance['client']['os'] == 'Linux'
    assert provenance['client']['route'] == 'Software server network namespace; localhost verified TLS/RESP3'
    assert provenance['server']['cpu_limit'] == provenance['client']['cpu_limit'] == 4
    assert provenance['server']['container_memory_bytes'] == 8589934592
    assert provenance['client']['memory_bytes'] == 4294967296
    cleanup = json.loads((HERE / 'cleanup.json').read_text())
    assert cleanup and all(v is True for v in cleanup.values())
    manifest = json.loads((HERE / 'sha256.json').read_text())
    assert set(manifest) == {p.name for p in HERE.iterdir() if p.is_file() and p.name != 'sha256.json'}
    for name, digest in manifest.items():
        assert hashlib.sha256((HERE / name).read_bytes()).hexdigest() == digest, name
    print('PASS: all 324 trials, full matrices, chart arithmetic, released build identity, QPF, provenance and cleanup')


if __name__ == '__main__':
    main()
