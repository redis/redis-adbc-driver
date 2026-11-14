#!/usr/bin/env python3
"""Audit retained Go point-write diagnostic observations (offline; no Redis)."""
import hashlib
import json
import statistics
from pathlib import Path


def audit(directory):
    provenance = json.loads((directory / 'provenance.json').read_text())
    for name, expected in provenance['source_hashes'].items():
        assert hashlib.sha256((directory / name).read_bytes()).hexdigest() == expected
    assert provenance['automatic_retries'] == 0
    assert provenance['independent_complete_final_states_validated'] is True
    inputs = {name: json.loads((directory / f'{name}.json').read_text())
              for name in ('baseline', 'candidate')}
    expected_cases = []
    for kind in ('insert', 'update', 'delete'):
        case = {'operation': kind, 'samples_per_build': 32}
        for name, data in inputs.items():
            assert data['rows_per_fixture'] == 1000 and data['public_columns'] == 4
            assert data['control_columns'] == 1 and data['caller_retries'] == 0
            assert len(data['samples']) == len(data['foreground_calls']) == 96
            samples = [r for r in data['samples'] if r['operation'] == kind]
            assert sorted(r['index'] for r in samples) == list(range(1, 33))
            times = [r['milliseconds'] for r in samples]
            assert all(t > 0 for t in times)
            commands = {}
            for sample in samples:
                phase = data['foreground_calls'][f"{kind}_{sample['index']:02d}"]
                assert phase['steps'] == 1
                assert phase['step_milliseconds'] == sample['milliseconds']
                for category, metric in phase['round_trips'].items():
                    assert metric['calls'] >= 1 and metric['commands'] >= metric['calls']
                    total = commands.setdefault(category, {'calls': 0, 'commands': 0, 'milliseconds': 0})
                    for key in total:
                        total[key] += metric[key]
            case[name] = {'p50_ms': statistics.median(times), 'min_ms': min(times),
                          'max_ms': max(times),
                          'client_round_trips': sum(x['calls'] for x in commands.values()),
                          'commands_by_category': commands}
        case['baseline_over_candidate'] = case['baseline']['p50_ms'] / case['candidate']['p50_ms']
        expected_cases.append(case)
    actual = json.loads((directory / 'comparison.json').read_text())
    # Categories are summed in recorded phase order by the exporter. Float
    # additions can differ by a few ulps when iterating sample order instead.
    import math
    def same(left, right):
        if isinstance(left, dict):
            return left.keys() == right.keys() and all(same(left[k], right[k]) for k in left)
        if isinstance(left, list):
            return len(left) == len(right) and all(same(a, b) for a, b in zip(left, right))
        if isinstance(left, float):
            return math.isclose(left, right, rel_tol=1e-12, abs_tol=1e-12)
        return left == right
    assert same(actual['cases'], expected_cases)
    print('PASS: 96 baseline + 96 candidate Go diagnostic observations, complete coverage, source/input hashes and comparison arithmetic')


if __name__ == '__main__':
    audit(Path(__file__).parent / '2026-11-14')
