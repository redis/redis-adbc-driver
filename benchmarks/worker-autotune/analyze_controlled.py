#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Explain allocation/probe evidence in a controlled evaluation, including partial runs."""
import argparse
import collections
import gzip
import json
from pathlib import Path
import statistics


def summarize(record):
    history = record['allocation_history']
    warm = [h for h in history if h['measurement']]
    weights = collections.Counter()
    targets = collections.Counter()
    reasons = collections.Counter()
    for h in warm:
        state = h['tuning']
        weight = len(h['query_seconds'])
        weights[state.get('phase', 'legacy-unreported')] += weight
        targets[str(state['target_workers'])] += weight
        reasons[state['reason']] += 1
    targets_all = [h['tuning']['target_workers'] for h in history]
    retained = [h['tuning'].get('retained_workers') for h in history]
    total = sum(weights.values())
    return dict(case=[record[k] for k in ['deployment', 'rows', 'GOMAXPROCS', 'condition', 'arrival_mode', 'concurrency']],
                policy=record['policy'], repetition=record['repetition'],
                warm_phase_query_counts=dict(weights), warm_target_query_counts=dict(targets),
                warm_reason_snapshots=dict(reasons),
                holding_query_fraction=weights['holding'] / total,
                target_changes=sum(a != b for a, b in zip(targets_all, targets_all[1:])),
                retained_changes=sum(a is not None and b is not None and a > 0 and b > 0 and a != b for a, b in zip(retained, retained[1:])),
                final_tuning=history[-1]['tuning'])


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('evaluation', type=Path)
    args = p.parse_args()
    with gzip.open(args.evaluation / 'results.jsonl.gz', 'rt') as stream:
        records = [json.loads(line) for line in stream]
    gate = json.loads((args.evaluation / 'gate.json').read_text())
    manifest = json.loads((args.evaluation / 'manifest.json').read_text())
    allocations = [summarize(r) for r in records if r['policy'] in ['tuned', 'legacy-tuned']]
    (args.evaluation / 'allocation-summary.json').write_text(json.dumps(allocations, indent=2) + '\n')
    groups = collections.defaultdict(list)
    for a in allocations:
        if a['policy'] == 'tuned':
            groups[tuple(a['case'])].append(a)
    lines = ['# Allocation and readiness evidence', '',
             f"Source: `{manifest['source_commit']}`. Gate: **{gate['status']}**. "
             f"{sum(c['passed'] for c in gate['cases'])}/{len(gate['cases'])} cases pass; "
             f"{len(gate['infrastructure'])} incomplete or invalid cases; {len(records)} recorded results.", '',
             'Holding is a diagnostic phase, not proof of convergence or a global optimum. '
             'Fractions weight snapshots by the number of completed queries. Target changes cover learning and measurement; holding fractions cover measurement. Reason counts '
             'are snapshots, not counts of independent decisions. Continuous snapshots can '
             'overlap other in-flight queries.', '',
             '| Case: deployment / rows / CPU / link / arrivals / concurrency | Holding fraction median (range) | Target changes median (range) | Final retained workers by repetition |',
             '|---|---:|---:|---|']
    for key, trials in sorted(groups.items()):
        fractions = [t['holding_query_fraction'] for t in trials]
        changes = [t['target_changes'] for t in trials]
        finals = [t['final_tuning'].get('retained_workers') for t in sorted(trials, key=lambda t: t['repetition'])]
        lines.append('| ' + ' / '.join(map(str, key)) + f' | {statistics.median(fractions):.2f} ({min(fractions):.2f}–{max(fractions):.2f})'
                     + f' | {statistics.median(changes):g} ({min(changes)}–{max(changes)}) | {finals} |')
    lines += ['', '## Old-policy diagnostic comparisons', '',
              'Both policies use the same profiler, fixtures, quotas and randomized experiment. '
              'These comparisons cannot retroactively control the original workstation run. '
              'Ratios are new policy / old policy medians; above 1 favors throughput, below 1 favors p95.', '',
              '| Case | QPS ratio | p95 ratio |', '|---|---:|---:|']
    for case in gate['cases']:
        if comparison := case.get('legacy_comparison'):
            lines.append('| ' + ' / '.join(map(str, case['case'])) + f" | {comparison['qps_ratio']:.3f} | {comparison['p95_ratio']:.3f} |")
    lines += ['', '## Interpretation limits', '',
              'The warmed gate uses the same highest-throughput fixed policy for its throughput '
              'and p95 reference. Server-transition windows include adaptation, rather than a '
              'stationary server. Cold learning excludes fixture loading/reference execution. '
              'A fresh handle is used per trial and shared across concurrency phases.', '',
              'The plan requires three repetitions. Tables show only collected repetitions; '
              'missing repetitions cannot establish uncertainty or readiness. Completed-case '
              'uncertainty uses full min/max spread, not a '
              'confidence interval. Host/VM activity and proxy overhead remain possible contributors. '
              'An equal retained worker count alone cannot establish that any observed regression '
              'was caused by the tuner; check grants, phase histories, CPU/throttling, and run spread.', '',
              'See `REPORT.md` and `gate.json` for every bound and resource summary, '
              '`results.jsonl.gz` for complete per-query and allocation evidence, '
              '`manifest.json` for the predeclared experiment, and `completion.json` for infrastructure results.']
    (args.evaluation / 'ALLOCATION.md').write_text('\n'.join(lines) + '\n')

if __name__ == '__main__':
    main()
