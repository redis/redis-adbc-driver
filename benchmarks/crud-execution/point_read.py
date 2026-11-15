"""Explicit 24-record native point-read target screen; not the full scenario suite."""
import argparse
import hashlib
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'table-modes'))
import scenarios


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('driver', 'cli', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    args = parser.parse_args()
    # Selection only: execute the original timer, normal driver/raw calls,
    # ownership/QPF setup and complete typed-result/source-state oracles.
    args.suite = 'oltp'
    args.repeats, args.operations, args.queries = 3, 32, 8
    args.point_rows, args.olap_rows = 1000, 100000
    scenarios.POINT = ('point_read',)
    benchmark = scenarios.ScenarioBenchmark(args)
    benchmark.results['metadata'].update(
        benchmark_kind='point_read_target_screen', operation_selection=['point_read'],
        wrapper_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
    benchmark.save()
    try:
        benchmark.run()
    except Exception as error:
        benchmark.results['status'] = 'failed'
        benchmark.results['failures'].append({'error_type': type(error).__name__})
        benchmark.save()
        raise


if __name__ == '__main__':
    main()
