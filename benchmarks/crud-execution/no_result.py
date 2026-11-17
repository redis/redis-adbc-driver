"""Explicit public DBAPI no-result caller experiment for issue #447.

Only the three declared mutation scenarios choose executemany(sql, None).
The unchanged scenario body owns SQL, counts, complete state checks and clocks.
No SQL inspection, driver change, retry or extra close is involved.
"""
import argparse
import hashlib
from pathlib import Path
import sys


MUTATIONS = frozenset(('insert', 'update', 'delete'))
PATHS = ('execute', 'executemany')


class NoResultCursor:
    def __init__(self, cursor):
        self._cursor = cursor

    def execute(self, sql):
        # Public manager 1.13.0 extension: None means execute exactly once,
        # whereas [] would execute zero times. Cancellation is manager-owned.
        return self._cursor.executemany(sql, None)

    def __getattr__(self, name):
        return getattr(self._cursor, name)


def install(scenarios, execution):
    if execution not in PATHS:
        raise ValueError('Unknown explicit mutation execution path')
    original_call = scenarios.operation_call
    original_init = scenarios.ScenarioBenchmark.__init__

    def call(cfg, mode, operation, index, rows, rowids, q, r):
        if execution == 'executemany' and mode in ('managed', 'unmanaged') and operation in MUTATIONS:
            q = NoResultCursor(q)
        return original_call(cfg, mode, operation, index, rows, rowids, q, r)

    def initialize(self, args):
        original_init(self, args)
        self.results['metadata']['mutation_execution'] = execution
        self.results['metadata']['no_result_wrapper_sha256'] = hashlib.sha256(
            Path(__file__).read_bytes()).hexdigest()
        self.save()

    scenarios.operation_call = call
    scenarios.ScenarioBenchmark.__init__ = initialize


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--mutation-execution', choices=PATHS, required=True)
    parser.add_argument('--diagnostics', action='store_true', help='Separate instrumented run; never use as comparison evidence')
    args, remaining = parser.parse_known_args()
    sys.argv = [sys.argv[0], *remaining]
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'table-modes'))
    import scenarios
    if args.diagnostics:
        import native_stages
        native_stages.install(scenarios)
    install(scenarios, args.mutation_execution)
    scenarios.main()


if __name__ == '__main__':
    main()
