"""Offline full-matrix, chart regeneration and provenance audit; no Redis access."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
sys.path.insert(0, str(HERE.parent))
import chart_data

def main():
    bulk = json.loads((HERE / 'table-modes.json').read_text())
    scenarios = json.loads((HERE / 'scenarios.json').read_text())
    assert (len(bulk['reads']), len(bulk['writes']), len(bulk['creation'])) == (84, 40, 32)
    assert len(scenarios['trials']) == 168
    summary = chart_data.table_summary(bulk)
    assert summary == json.loads((HERE / 'summary.json').read_text())
    expected = chart_data.make_charts(bulk, scenarios)
    paths = {
        'table_modes': HERE / 'table-modes.json',
        'scenarios': HERE / 'scenarios.json',
        'historical_clickbench': ROOT / 'benchmarks/clickbench/full-c6a-cluster-2026-10-21/early-comparison/comparison.json',
        'historical_aggregates': ROOT / 'benchmarks/exact-aggregate-406/2026-10-21/full/summary.json',
    }
    chart_data.add_historical(expected, json.loads(paths['historical_clickbench'].read_text()))
    chart_data.add_historical_aggregates(expected, json.loads(paths['historical_aggregates'].read_text()))
    expected['input_sha256'] = {k: hashlib.sha256(p.read_bytes()).hexdigest() for k, p in paths.items()}
    assert expected == json.loads((HERE / 'charts.json').read_text())
    with tempfile.TemporaryDirectory() as temp:
        out = Path(temp) / 'charts'
        assert chart_data.write_outputs(expected, out) == 84
        for file in out.iterdir():
            assert file.read_bytes() == (HERE / file.name).read_bytes(), file.name
    provenance = json.loads((HERE / 'provenance.json').read_text())
    for data in [bulk, scenarios]:
        assert data['metadata']['driver_source_commit'] == provenance['source_commit']
        assert data['metadata']['driver_sha256'] == provenance['sha256']['library']
        assert data['metadata']['driver_version'] == provenance['version']
    assert provenance['source_commit'] == '34f4c82972c7a059f045f1fb65bdfc2ce4adfce9'
    assert provenance['source_clean_before_timing']
    assert provenance['driver_options']['adbc.redis.writer_admission_queue'] is False
    assert provenance['client']['os'] == 'Linux' and provenance['client']['cpu_limit'] == 4
    assert provenance['server']['qpf'] == 4 and provenance['server']['search_workers'] == 6
    for name in ['run.py', 'scenarios.py', 'arrow_compare.py']:
        assert hashlib.sha256((HERE.parent / name).read_bytes()).hexdigest() == provenance['sha256'][name]
    cleanup = json.loads((HERE / 'cleanup.json').read_text())
    assert set(cleanup) == {
        'all_benchmark_clients_exited',
        'bulk_and_scenario_owner_markers_verified_before_flush',
        'database_empty_and_index_free_verified_over_tls_resp3',
        'owned_server_container_and_network_removed',
        'preexisting_container_inventory_preserved',
        'unowned_network_ids_and_configuration_preserved_from_after_setup_inventory',
        'new_client_image_tag_removed',
        'generated_tls_and_environment_files_removed',
        'generated_credential_and_owner_files_removed',
    }
    assert all(value is True for value in cleanup.values()), cleanup
    manifest = json.loads((HERE / 'sha256.json').read_text())
    assert set(manifest) == {
        p.name for p in HERE.iterdir() if p.is_file() and p.name != 'sha256.json'
    }
    for name, digest in manifest.items():
        assert hashlib.sha256((HERE / name).read_bytes()).hexdigest() == digest, name
    print('PASS: 324 complete validated trials, 84 regenerated chart rows, exact build/fixture/QPF, cleanup and hashes')

if __name__ == '__main__':
    main()
