"""Reproduce the two-mode control using unchanged full-value read workers.

Use an owned empty Software fixture with verified 4x QPF, TLS and RESP3. Only
reads are allowed. Cleanup checks the harness ownership marker; no provisioning
or infrastructure teardown occurs. Cleanup is outside every timed trial.
"""
import argparse
import sys

import arrow_compare

arrow_compare.base.MODES = ('managed', 'unmanaged')


class Benchmark(arrow_compare.Benchmark):
    def run_reads(self):
        try:
            super().run_reads()
        finally:
            with arrow_compare.base.client(self.cfg) as client:
                assert client.get(arrow_compare.base.MARKER) == self.owner.encode()
                for index in client.execute_command('FT._LIST'):
                    client.execute_command('FT.DROPINDEX', index)
                client.flushdb()
                assert client.dbsize() == 0 and not client.execute_command('FT._LIST')


arrow_compare.Benchmark = Benchmark

if __name__ == '__main__':
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--suite', choices=['reads'], default='reads')
    parser.parse_known_args()
    if not any(arg == '--suite' or arg.startswith('--suite=') for arg in sys.argv[1:]):
        sys.argv.extend(('--suite', 'reads'))
    arrow_compare.main()
