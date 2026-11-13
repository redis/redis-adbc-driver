"""Native managed Arrow append control for compaction-control experiments.

Uses the existing independently validated table-mode workers and timing boundary.
Run each pinned library in a fresh process/work directory on an owned empty
Software fixture. The caller retains responsibility for fixture cleanup.
"""
import argparse
from pathlib import Path

import arrow_compare


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'driver', 'cli', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    parser.add_argument('--read-repeats', type=int, default=6)
    parser.add_argument('--write-repeats', type=int, default=3)
    parser.add_argument('--write-rows', type=int, default=10000)
    parser.add_argument('--sql-batch-rows', type=int, default=1000)
    args = parser.parse_args()
    if args.write_rows != 10000 or args.sql_batch_rows != 1000 or args.write_repeats <= 0:
        parser.error('Use the fixed 10K-row / 1K-call control and positive repetitions')
    benchmark = arrow_compare.Benchmark(args)
    benchmark.results['metadata']['scope'] = 'Managed Arrow compaction-control comparison; 1K public-call units'
    benchmark.save()
    try:
        benchmark.write_cases('writes', [('managed', 'arrow', writers, 'append') for writers in (1, 4)])
        benchmark.results['final_qpf'] = benchmark.gate()
        benchmark.results['status'] = 'complete'
        benchmark.save()
    except Exception as error:
        benchmark.results['status'] = 'failed'
        benchmark.results['failures'].append({'error_type': type(error).__name__})
        benchmark.save()
        raise


if __name__ == '__main__':
    main()
