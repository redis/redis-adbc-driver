"""Summarize complete, independently validated paired screens; no proxy timing mix."""
import argparse
import json
from pathlib import Path
import statistics


def summarize(report):
    if report['status'] != 'passed' or report.get('smoke') or report.get('wire_diagnostic'):
        raise ValueError('A completed direct promotion screen is required')
    if not all(t['independent_full_values_validated'] for t in report['trials']):
        raise ValueError('Every warm-up and measured fetch must be independently validated')
    if not all(t['active_readers'] == t['readers'] and t['rows'] == 100000 for t in report['trials']):
        raise ValueError('Every requested reader must consume a nonempty share of the complete fixture')
    groups = {}
    for trial in report['trials']:
        key = (trial['projection'], trial['api'], trial['readers'])
        groups.setdefault(key, {'baseline': [], 'candidate': []})[trial['build']].append(trial)
    if len(groups) != 8:
        raise ValueError('Wide/narrow, regular/partition, one/four-reader matrix is incomplete')
    summaries = []
    for key, methods in sorted(groups.items()):
        result = dict(zip(['projection', 'api', 'readers'], key))
        measured = {}
        for name, all_trials in methods.items():
            warmups = [t for t in all_trials if t['warmup']]
            trials = [t for t in all_trials if not t['warmup']]
            repeats = [t['repeat'] for t in trials]
            if len(warmups) != 1 or len(trials) < 5 or len(set(repeats)) != len(repeats):
                raise ValueError('Need exactly one warm-up and at least five unique measured trials')
            measured[name] = {t['repeat']: t for t in trials}
            seconds = [t['seconds'] for t in trials]
            result[name] = {'elapsed_median_seconds': statistics.median(seconds),
                            'elapsed_range_seconds': [min(seconds), max(seconds)],
                            'trials': len(trials),
                            'actual_first_batch_rows': sorted({m['FirstRows'] for t in trials for m in t['metrics']})}
            for metric in ['client_cpu_seconds', 'allocated_bytes', 'allocations',
                           'sampled_peak_go_heap_bytes', 'process_lifetime_peak_rss_bytes']:
                result[name][metric+'_median'] = statistics.median(t[metric] for t in trials)
            if all('sampled_process_peak_rss_bytes' in t for t in trials):
                result[name]['sampled_process_peak_rss_bytes_median'] = statistics.median(
                    t['sampled_process_peak_rss_bytes'] for t in trials)
        if measured['baseline'].keys() != measured['candidate'].keys():
            raise ValueError('Paired repeat identities differ')
        pairs = [(measured['baseline'][r], measured['candidate'][r]) for r in measured['baseline']]
        result['candidate_paired_elapsed_wins'] = sum(c['seconds'] < b['seconds'] for b, c in pairs)
        result['median_elapsed_change_percent'] = 100*(result['candidate']['elapsed_median_seconds']/result['baseline']['elapsed_median_seconds']-1)
        summaries.append(result)
    return {'source_sha256': report['source_sha256'], 'binaries': report['binaries'], 'cases': summaries,
            'interpretation': 'Go public ADBC APIs and Go Arrow allocator, including IPC output. Negative elapsed change is faster. Sampled heap is not a strict peak; RSS is process lifetime high-water. No native server-export speedup or Python/C-boundary speedup is established.'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('results', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(json.dumps(summarize(json.loads(args.results.read_text())), indent=2))
