"""Opt-in native known-key projection width comparison on owned Software 4x QPF.

No live invocation is implied by source creation. Operational credentials, the
trusted CA and endpoint are supplied privately through REDIS_BENCH_*.
"""
import argparse
import concurrent.futures
from contextlib import ExitStack
import hashlib
import itertools
import json
import statistics
from pathlib import Path
import sys
import threading
import time

import pyarrow as pa

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'table-modes'))
import scenarios
import run as base

PAYLOAD_BYTES = 64 << 10
WIDTHS = ('none', 'registered', 'unregistered', 'both')
MODES = ('unmanaged', 'redis_py')
SCHEMA = scenarios.SCHEMA
REGISTERED = 'registered_payload'
UNREGISTERED = 'unregistered_payload'


def record(i):
    return {'id': i, 'grp': i % 16, 'amount': 9007199254740993 + i,
            'label': f'row-{i:08d}' + chr(0) + 'label'}


def source_row(i, width):
    row = {**record(i), base.ORDINAL: i}
    if width in ('registered', 'both'):
        row[REGISTERED] = 'r' * PAYLOAD_BYTES
    if width in ('unregistered', 'both'):
        row[UNREGISTERED] = ('u' + chr(0)) * (PAYLOAD_BYTES // 2)
    return {name: str(value).encode() for name, value in row.items()}


def indices(operations, rows):
    return [(i * 61) % rows + 1 for i in range(operations)]


def byte_oracle(width, columns, operations, rows):
    selected = SCHEMA.names[:columns]
    keys = indices(operations, rows)
    return {
        'selected_value_bytes': sum(len(source_row(i, width)[name]) for i in keys for name in selected),
        'whole_hash_value_bytes': sum(sum(map(len, source_row(i, width).values())) for i in keys),
        'scope': 'Exact source value bytes for requested keys, excluding field names/RESP/TLS. Not measured wire bytes.',
    }


def worker(cfg, mode, columns, ids, barriers, release):
    latencies, values = [], []
    names = SCHEMA.names[:columns]
    schema = pa.schema([SCHEMA.field(name) for name in names])
    try:
        with ExitStack() as stack:
            r = stack.enter_context(base.client(cfg)) if mode == 'redis_py' else None
            c = stack.enter_context(base.connect(cfg)) if mode == 'unmanaged' else None
            q = stack.enter_context(c.cursor()) if c is not None else None
            barriers[0].wait(timeout=180)
            barriers[1].wait(timeout=180)
            for i in ids:
                start = time.perf_counter_ns()
                key = base.PREFIX + str(i)
                if mode == 'redis_py':
                    raw = r.hmget(key, names)
                    assert all(value is not None for value in raw)
                    row = {name: (value.decode() if name == 'label' else int(value))
                           for name, value in zip(names, raw)}
                    table = pa.Table.from_pylist([row], schema=schema)
                else:
                    q.execute('SELECT ' + ','.join(names) + ' FROM bench.unmanaged WHERE __key=' + base.literal(key))
                    table = q.fetch_arrow_table()
                latencies.append(time.perf_counter_ns() - start)
                values.append((i, table))
            barriers[2].wait(timeout=180)
            assert release.wait(timeout=180)
        return {'latency_ns': latencies, 'values': values}
    except Exception as error:
        for barrier in barriers:
            barrier.abort()
        release.set()
        return {'error_type': type(error).__name__, 'latency_ns': latencies}


class Benchmark(scenarios.ScenarioBenchmark):
    def __init__(self, args):
        super().__init__(args)
        metadata = self.results['metadata']
        for key in ('queries', 'olap_rows'):
            metadata.pop(key, None)
        metadata.update(
            suite='unmanaged_known_key_projection', widths=list(WIDTHS), projection_columns=[1, 4],
            clients=[1, 4], modes=list(MODES), payload_bytes=PAYLOAD_BYTES,
            generator='id=1..N; grp=id%16; amount=9007199254740993+id; label=row-{id:08d} + NUL + label',
            harness_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            cli_sha256=hashlib.sha256(args.cli.read_bytes()).hexdigest(),
            helper_sha256={name: hashlib.sha256(Path(module.__file__).read_bytes()).hexdigest()
                           for name, module in [('scenarios.py', scenarios), ('table-modes/run.py', base)]},
            timing_scope='Persistent client/cursor objects; SQL execute plus full Arrow fetch or HMGET plus equivalent typed Arrow conversion. Lazy first transport work included. Setup/teardown and independent validation excluded.',
            byte_scope='Value-byte oracles only; no measured packet/wire-byte attribution.')
        self.results['fixtures'] = []
        self.save()

    def setup_width(self, width):
        self.reset()
        schema = SCHEMA
        if width in ('registered', 'both'):
            schema = schema.append(pa.field(REGISTERED, pa.string()))
        # Every width uses the same index definition; payload size cannot change
        # FT.INFO reply bytes or indexed term work in this experiment.
        with base.client(self.cfg) as r:
            r.execute_command('FT.CREATE', base.INDEX, 'ON', 'HASH', 'PREFIX', 1,
                              base.PREFIX, 'SCHEMA', 'id', 'NUMERIC')
            for lo in range(1, self.args.point_rows + 1, 128):
                with r.pipeline(transaction=False) as pipe:
                    for i in range(lo, min(lo + 128, self.args.point_rows + 1)):
                        row = source_row(i, width)
                        row.pop(UNREGISTERED, None)
                        pipe.hset(base.PREFIX + str(i), mapping=row)
                    pipe.execute()
        base.ready(self.cfg, self.args.point_rows)
        base.register(self.cfg, schema, self.work / 'registration-private.log')
        # Register the schema before introducing the intentionally unmodeled field.
        if width in ('unregistered', 'both'):
            with base.client(self.cfg) as r:
                for lo in range(1, self.args.point_rows + 1, 128):
                    with r.pipeline(transaction=False) as pipe:
                        for i in range(lo, min(lo + 128, self.args.point_rows + 1)):
                            pipe.hset(base.PREFIX + str(i), UNREGISTERED, source_row(i, width)[UNREGISTERED])
                        pipe.execute()
        self.validate_source(width)

    def validate_source(self, width):
        # Compare every binary field and every key, not a source sample or a
        # checksum of only the projected columns. Entirely outside trial clocks.
        all_bytes = 0
        with base.client(self.cfg) as r:
            assert sum(1 for _ in r.scan_iter(match=base.PREFIX + '*')) == self.args.point_rows
            for lo in range(1, self.args.point_rows + 1, 128):
                ids = list(range(lo, min(lo + 128, self.args.point_rows + 1)))
                with r.pipeline(transaction=False) as pipe:
                    for i in ids:
                        pipe.hgetall(base.PREFIX + str(i))
                    rows = pipe.execute()
                for i, actual in zip(ids, rows):
                    want = {k.encode(): v for k, v in source_row(i, width).items()}
                    assert actual == want
                    all_bytes += sum(map(len, actual.values()))
        return all_bytes

    def trial(self, width, mode, columns, clients, repeat):
        self.gate()
        ids = indices(self.args.operations, self.args.point_rows)
        barriers = [threading.Barrier(clients + 1) for _ in range(3)]
        release = threading.Event()
        broken = False
        with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
            futures = [pool.submit(worker, self.cfg, mode, columns, ids[i::clients], barriers, release)
                       for i in range(clients)]
            try:
                barriers[0].wait(timeout=180)
                start = time.perf_counter_ns()
                barriers[1].wait(timeout=180)
                barriers[2].wait(timeout=180)
                elapsed = (time.perf_counter_ns() - start) / 1e9
            except threading.BrokenBarrierError:
                broken = True
                elapsed = None
            finally:
                release.set()
            workers = [future.result() for future in futures]
        samples = [v for result in workers for v in result.get('latency_ns', [])]
        row = {'width': width, 'mode': mode, 'projection_columns': columns, 'clients': clients,
               'repeat': repeat, 'warmup': repeat < 0, 'operations': self.args.operations,
               'fixture_rows': self.args.point_rows, 'seconds': elapsed, 'latency_ns': samples,
               'status': 'failed', 'validated': False,
               'byte_oracle': byte_oracle(width, columns, self.args.operations, self.args.point_rows)}
        self.results['trials'].append(row)
        self.save()
        errors = [result['error_type'] for result in workers if 'error_type' in result]
        if broken or errors:
            row['errors'] = errors
            self.save()
            raise RuntimeError('Failed point trial; no caller retries')
        expected_schema = pa.schema(list(SCHEMA)[:columns])
        outputs = [(i, table) for result in workers for i, table in result['values']]
        assert len(outputs) == self.args.operations
        assert sorted(i for i, _ in outputs) == sorted(ids)
        for i, table in outputs:
            want = pa.Table.from_pylist([record(i)], schema=SCHEMA).select(expected_schema.names)
            assert table.schema.equals(expected_schema, check_metadata=False)
            assert table.combine_chunks().equals(want, check_metadata=False)
        assert len(samples) == self.args.operations
        row.update(status='validated', validated=True,
                   median_latency_ms=statistics.median(samples) / 1e6,
                   p95_latency_ms=scenarios.percentile(samples, .95) / 1e6,
                   operations_per_second=self.args.operations / elapsed)
        self.save()
        print('POINT', width, mode, columns, clients, repeat, 'validated', flush=True)

    def run(self):
        for width in WIDTHS:
            self.setup_width(width)
            for repeat in range(-1, self.args.repeats):
                cases = list(itertools.product(MODES, (1, 4), (1, 4)))
                for mode, columns, clients in (cases if repeat % 2 else reversed(cases)):
                    self.trial(width, mode, columns, clients, repeat)
            source_bytes = self.validate_source(width)
            self.results['fixtures'].append({'width': width, 'source_preserved': True,
                                             'source_rows': self.args.point_rows,
                                             'complete_source_value_bytes': source_bytes})
            self.save()
        assert hashlib.sha256(self.args.driver.read_bytes()).hexdigest() == self.results['metadata']['driver_sha256']
        assert hashlib.sha256(self.args.cli.read_bytes()).hexdigest() == self.results['metadata']['cli_sha256']
        self.results['final_qpf'] = self.gate()
        self.results['status'] = 'complete'
        self.save()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('driver', 'cli', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    parser.add_argument('--repeats', type=int, default=3)
    parser.add_argument('--operations', type=int, default=32)
    parser.add_argument('--point-rows', type=int, default=1000)
    args = parser.parse_args()
    if args.repeats < 1 or args.operations < 4 or args.operations % 4 or args.point_rows < args.operations or args.point_rows % 61 == 0:
        parser.error('Invalid finite trial counts')
    # Reuse only the initial owned fixture/catalog/QPF setup. This specialized
    # run does not execute the standard chart matrix or claim its coverage.
    args.suite, args.queries, args.olap_rows = 'all', 8, 100000
    benchmark = Benchmark(args)
    try:
        benchmark.run()
    except Exception as error:
        benchmark.results['status'] = 'failed'
        benchmark.results['failures'].append({'error_type': type(error).__name__})
        benchmark.save()
        raise


if __name__ == '__main__':
    main()
