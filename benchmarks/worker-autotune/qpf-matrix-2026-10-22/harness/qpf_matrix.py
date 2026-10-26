#!/usr/bin/env python3
"""Compare Standard/2x/4x/8x on one disposable Software database.

Pass qpf.py's common arguments after --output. QPF changes are explicit here;
the final setting is 4x. The server must have enough CPU for 8x throughout.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys


def summarize(output):
    lines = ['# QPF and worker policy comparison', '',
             'Tuned / highest-throughput fixed policy, using medians across repeated trials. '
             'QPF is blocked in order 2x, Standard, 8x, 4x; residual time/order effects are possible.', '',
             '| QPF | Arrivals | Concurrent statements | Best fixed | Tuned QPS | Fixed QPS | QPS ratio | p95 ratio | Gate |',
             '|---|---|---:|---|---:|---:|---:|---:|---|']
    for factor in [0, 2, 4, 8]:
        path = output / f'qpf-{factor}' / 'gate.json'
        if not path.exists():
            continue
        for case in json.loads(path.read_text())['cases']:
            policies = case['policies']
            tuned = policies['tuned']['queries_per_second']['median']
            fixed = policies[case['best_fixed']]['queries_per_second']['median']
            lines.append(f'| {factor or "Standard"} | {case["case"][-2]} | {case["case"][-1]} | {case["best_fixed"]} | '
                         f'{tuned:.2f} | {fixed:.2f} | {case["steady_qps_ratio"]:.3f} | '
                         f'{case["steady_p95_ratio"]:.3f} | {case["passed"]} |')
    lines += ['', 'One mixed aggregate, direct connection. See each factor manifest for row counts, '
              'server topology, client/server limits, repetitions, and phase lengths. '
              'P95 is coarse at low concurrency. '
              'This is a local policy comparison, not general readiness or partition throughput evidence. '
              'See each factor manifest, full min/max spreads, allocation histories and correctness logs.']
    (output / 'REPORT.md').write_text('\n'.join(lines) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args, common = parser.parse_known_args()
    if any(arg.split('=', 1)[0] in ['--qpf', '--configure-qpf'] for arg in common):
        parser.error('the matrix owns --qpf and --configure-qpf; omit these common arguments')
    args.output.mkdir(parents=True, exist_ok=False)
    factors = [2, 0, 8, 4]
    (args.output / 'matrix.json').write_text(json.dumps(dict(factors=factors, arguments=common), indent=2) + '\n')
    statuses = {}
    for factor in factors:
        result = subprocess.run([sys.executable, str(Path(__file__).with_name('qpf.py')),
                                 '--output', str(args.output / f'qpf-{factor}'), '--qpf', str(factor),
                                 '--configure-qpf', *common])
        statuses[factor] = result.returncode
        (args.output / 'completion.json').write_text(json.dumps(statuses, indent=2) + '\n')
        if result.returncode not in (0, 2):
            break
    summarize(args.output)
    raise SystemExit(1 if len(statuses) != 4 or any(s not in (0, 2) for s in statuses.values())
                     else 2 if 2 in statuses.values() else 0)


if __name__ == '__main__':
    main()
