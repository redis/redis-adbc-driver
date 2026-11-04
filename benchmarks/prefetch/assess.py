"""Describe paired elapsed uncertainty for a completed matched prefetch screen.

The seeded paired bootstrap describes this fixture/run, not a production-wide
confidence guarantee. Keep every trial and the declared decision rule alongside
this summary; do not hide narrow controls, failures or outliers.
"""
import argparse
import json
from pathlib import Path
import random
import statistics

from summarize import summarize


def paired_interval(changes, draws=10000):
    if len(changes) < 5:
        raise ValueError('At least five paired measurements required')
    rng = random.Random(193)
    medians = sorted(statistics.median(rng.choices(changes, k=len(changes))) for _ in range(draws))
    return [medians[int(0.025*draws)], medians[int(0.975*draws)]]


def assess(report):
    summary = summarize(report)
    for case in summary['cases']:
        trials = [t for t in report['trials'] if not t['warmup'] and
                  all(t[k] == case[k] for k in ['projection', 'api', 'readers'])]
        methods = {name: {t['repeat']: t for t in trials if t['build'] == name}
                   for name in ['baseline', 'candidate']}
        changes = [100*(methods['candidate'][r]['seconds']/baseline['seconds']-1)
                   for r, baseline in methods['baseline'].items()]
        case['paired_elapsed_change_percent'] = changes
        case['paired_median_elapsed_change_percent'] = statistics.median(changes)
        case['paired_bootstrap_median_interval_95_percent'] = paired_interval(changes)
        case['within_declared_5_percent_regression_margin'] = case['paired_bootstrap_median_interval_95_percent'][1] <= 5
        for name in ['baseline', 'candidate']:
            case[name]['minimum_rss_samples'] = min(t['process_rss_samples'] for t in methods[name].values())
            case[name]['maximum_sampled_process_rss_bytes'] = max(t['sampled_process_peak_rss_bytes'] for t in methods[name].values())
    summary['wide_columns'] = report.get('wide_columns', 105)
    summary['wide_elapsed_gate_passed'] = all(
        c['within_declared_5_percent_regression_margin'] and
        (c['readers'] != 1 or c['paired_bootstrap_median_interval_95_percent'][1] < 0)
        for c in summary['cases'] if c['projection'] == 'wide')
    summary['uncertainty_method'] = '10000 seeded bootstrap resamples of paired percentage changes; descriptive fixture/run interval, not a universal performance guarantee'
    return summary


if __name__ == '__main__':
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('results', type=Path)
    ap.add_argument('--output', type=Path, required=True)
    args = ap.parse_args()
    args.output.write_text(json.dumps(assess(json.loads(args.results.read_text())), indent=2)+'\n')
