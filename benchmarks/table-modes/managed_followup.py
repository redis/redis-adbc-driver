"""Managed follow-up: matched reads/writes and separately labeled Arrow call sizes."""
import argparse
from pathlib import Path

import arrow_compare


class Benchmark(arrow_compare.Benchmark):
    def __init__(self, args):
        super().__init__(args)
        self.results['metadata'].update(
            scope='Managed follow-up: matched reads/writes plus separately labeled larger Arrow calls',
            additional_arrow_call_rows=args.large_arrow_call_rows,
            read_modes=['managed'],
            framework_write_paths=self.results['metadata']['write_paths'],
            write_paths=['managed_sql', 'managed_arrow'])
        self.save()

    def run_reads(self):
        previous = arrow_compare.base.MODES
        try:
            arrow_compare.base.MODES = ('managed',)
            super().run_reads()
        finally:
            arrow_compare.base.MODES = previous

    def run_writes(self):
        self.write_cases('writes', [('managed', api, n, 'append')
                                   for n in (1, 4) for api in ('sql', 'arrow')])
        previous = self.args.sql_batch_rows
        try:
            self.args.sql_batch_rows = self.args.large_arrow_call_rows
            self.write_cases('writes', [('managed', 'arrow', n, 'append') for n in (1, 4)])
        finally:
            self.args.sql_batch_rows = previous


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'driver', 'cli', 'work'):
        p.add_argument('--' + name, type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--dedicated-empty-server', action='store_true', required=True)
    p.add_argument('--read-repeats', type=int, default=6)
    p.add_argument('--write-repeats', type=int, default=3)
    p.add_argument('--write-rows', type=int, default=10000)
    p.add_argument('--sql-batch-rows', type=int, default=1000)
    p.add_argument('--large-arrow-call-rows', type=int, default=10000)
    args = p.parse_args()
    if (not 4 <= args.write_rows <= 100000 or args.write_rows % 4 or
            min(args.read_repeats, args.write_repeats, args.sql_batch_rows) <= 0 or
            args.large_arrow_call_rows <= args.sql_batch_rows):
        p.error('Invalid rows/repetitions or larger Arrow call ceiling')
    b = Benchmark(args)
    try:
        b.run_reads()
        b.run_writes()
        b.results['final_qpf'] = b.gate()
        b.results['status'] = ('complete' if all(r['validated'] for r in b.results['writes'])
                               else 'complete_with_write_conflicts')
        b.save()
    except Exception as error:
        b.results['status'] = 'failed'
        b.results['failures'].append({'error_type': type(error).__name__})
        b.save()
        raise


if __name__ == '__main__':
    main()
