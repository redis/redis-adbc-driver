"""Offline, complete regeneration audit for the scenario/chart evidence bundle."""
import hashlib
import json
from pathlib import Path
import tempfile

import chart_data as charts


def main():
    root=Path(__file__).parent;bundle=root/'scenario-charts-2026-11-13'
    paths={'table_modes':root/'merged-scoped-2026-11-13/measurements.json',
           'scenarios':bundle/'measurements.json',
           'historical_clickbench':root.parent/'clickbench/full-c6a-cluster-2026-10-21/early-comparison/comparison.json',
           'historical_aggregates':root.parent/'exact-aggregate-406/2026-10-21/full/summary.json'}
    data={k:json.loads(p.read_text()) for k,p in paths.items()}
    expected=charts.make_charts(data['table_modes'],data['scenarios'])
    charts.add_historical(expected,data['historical_clickbench'])
    charts.add_historical_aggregates(expected,data['historical_aggregates'])
    expected['input_sha256']={k:hashlib.sha256(p.read_bytes()).hexdigest() for k,p in paths.items()}
    assert expected==json.loads((bundle/'charts.json').read_text())
    with tempfile.TemporaryDirectory() as directory:
        out=Path(directory)/'regenerated';assert charts.write_outputs(expected,out)==84
        for p in out.iterdir():assert p.read_bytes()==(bundle/p.name).read_bytes(),p.name
    pilot=json.loads((bundle/'pilot.json').read_text());charts.scenario_summary(pilot)
    assert len(pilot['trials'])==84 and len(data['scenarios']['trials'])==168
    provenance=json.loads((bundle/'provenance.json').read_text());m=data['scenarios']['metadata']
    assert provenance['measured_harness']['sha256']==m['harness_sha256']
    assert all(provenance['driver'][k]==m[k] for k in ('driver_version','driver_source_commit','driver_sha256'))
    assert provenance['software_version']=='8.2.6-25' and provenance['redis_version']=='8.8.0'
    assert provenance['search_module_version']==80808 and provenance['server_cpus']==4
    assert provenance['container_memory_bytes']==8589934592 and provenance['protocol']==3
    cleanup=json.loads((bundle/'cleanup.json').read_text())
    assert all(cleanup[k] is True for k in ('owned_container_and_network_removed','preexisting_containers_preserved',
               'preexisting_networks_preserved','generated_tls_and_environment_removed','private_generated_credentials_removed'))
    print('PASS: 168 standard +84 pilot records, 156 prior table-mode records; exact 84-row chart JSON/CSV and separate 44/8 historical series; provenance and cleanup')


if __name__=='__main__':main()
