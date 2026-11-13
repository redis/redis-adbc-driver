"""Matched managed-only native reads, with complete IPC output validation."""

import argparse
import concurrent.futures
import json
import multiprocessing
from pathlib import Path
import tempfile
import time

import pyarrow as pa

import run as base


class Benchmark(base.Benchmark):
    def run_reads(self):
        self.results['metadata'].update(scope='Managed native read memory', automatic_retries=0)
        self.reset()
        base.setup_managed(self.cfg, self.expected)
        self.validate_sql('managed', self.expected)
        cases = [(readers, batch) for batch in (512, 4096) for readers in (1, 4)]
        for rep in range(-1, self.args.read_repeats):
            for readers, batch in (cases if rep % 2 else reversed(cases)):
                self.gate()
                with tempfile.TemporaryDirectory(dir=self.work) as tmp:
                    tasks = 1 if readers == 1 else 8
                    paths = [str(Path(tmp) / f'{i}.arrow') for i in range(tasks)]
                    payloads = [(self.cfg, 'managed', i*100000//tasks+1,
                                 (i+1)*100000//tasks, batch, paths[i]) for i in range(tasks)]
                    start = time.perf_counter()
                    with concurrent.futures.ProcessPoolExecutor(
                            readers, mp_context=multiprocessing.get_context('spawn')) as pool:
                        workers = list(pool.map(base.read_worker, payloads))
                    elapsed = time.perf_counter() - start
                    actual = pa.concat_tables([pa.ipc.open_stream(path).read_all() for path in paths])
                    assert base.canonical(actual, self.expected.schema).equals(
                        self.sorted, check_metadata=False)
                row = {'mode': 'managed', 'readers': readers, 'batch_rows': batch,
                       'repeat': rep, 'warmup': rep < 0, 'seconds': elapsed,
                       'first_batch_seconds': min(w['first_batch_at'] for w in workers)-start,
                       'rows': 100000, 'columns': 105, 'tasks': tasks, 'validated': True,
                       'peak_worker_rss_bytes': max(w['peak_rss_bytes'] for w in workers)}
                self.results['reads'].append(row)
                self.save()
                print('READ', json.dumps(row), flush=True)
        self.results['final_qpf'] = self.gate()
        self.results['status'] = 'complete'
        self.save()

    def cleanup(self):
        with base.client(self.cfg) as client:
            assert client.get(base.MARKER) == self.owner.encode()
            for index in client.execute_command('FT._LIST'):
                client.execute_command('FT.DROPINDEX', index)
            client.flushdb()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'driver', 'cli', 'work'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    parser.add_argument('--read-repeats', type=int, default=6)
    parser.set_defaults(write_rows=10000, write_repeats=0, sql_batch_rows=1000)
    args = parser.parse_args()
    if args.read_repeats < 1:
        parser.error('read repetitions must be positive')
    benchmark = Benchmark(args)
    try:
        benchmark.run_reads()
    except Exception as error:
        benchmark.results['status'] = 'failed'
        benchmark.results['failures'].append({'error_type': type(error).__name__})
        benchmark.save()
        raise
    finally:
        benchmark.cleanup()


if __name__ == '__main__':
    main()
