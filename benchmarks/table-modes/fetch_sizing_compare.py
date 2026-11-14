"""Compare read page sizes with full wide and managed narrow projections."""
import argparse
import concurrent.futures
import multiprocessing
from pathlib import Path
import tempfile
import sys
import time
import pyarrow as pa
import arrow_compare

base = arrow_compare.base
original_read_worker = base.read_worker


def read_worker(payload):
    """Keep the original worker, projecting its input schema in this process."""
    cfg = payload[0]
    original_source = base.source
    if cfg['projection_columns'] == 8:
        base.source = lambda path: original_source(path).select(range(8))
    try:
        return original_read_worker(payload)
    finally:
        base.source = original_source


class Benchmark(arrow_compare.Benchmark):
    def run_reads(self):
        try:
            self.reset()
            base.setup_managed(self.cfg, self.expected)
            base.raw_create(self.cfg, self.expected.schema)
            base.write_worker((self.cfg, 'redis_py', 0, 100000, 1000))
            base.ready(self.cfg, 100000)
            base.register(self.cfg, self.expected.schema, self.work/'read-registration-private.log')
            self.validate_sql('managed', self.expected)
            self.validate_sql('unmanaged', self.expected)
            self.validate_raw(self.expected)
            cases = [(width, mode, readers, batch)
                     for width in (8, 105)
                     for mode in (('managed',) if width == 8 else ('managed', 'unmanaged'))
                     for readers in (1, 4) for batch in (512, 4096)]
            for rep in range(-1, self.args.read_repeats):
                for width, mode, readers, batch in (cases if rep % 2 else reversed(cases)):
                    self.gate()
                    expected = self.expected.select(range(width))
                    expected_sorted = base.canonical(expected, expected.schema)
                    with tempfile.TemporaryDirectory(dir=self.work) as tmp:
                        tasks = 1 if readers == 1 else 8
                        paths = [str(Path(tmp)/f'{i}.arrow') for i in range(tasks)]
                        cfg = {**self.cfg, 'projection_columns': width}
                        payloads = [(cfg, mode, i*100000//tasks+1,
                                     (i+1)*100000//tasks, batch, paths[i]) for i in range(tasks)]
                        start = time.perf_counter()
                        with concurrent.futures.ProcessPoolExecutor(
                                readers, mp_context=multiprocessing.get_context('spawn')) as pool:
                            workers = list(pool.map(read_worker, payloads))
                        elapsed = time.perf_counter()-start
                        actual = pa.concat_tables([pa.ipc.open_stream(path).read_all() for path in paths])
                        assert base.canonical(actual, expected.schema).equals(expected_sorted, check_metadata=False)
                    self.results['reads'].append({
                        'mode': mode, 'readers': readers, 'batch_rows': batch, 'repeat': rep,
                        'warmup': rep < 0, 'seconds': elapsed, 'rows': 100000, 'columns': width,
                        'tasks': tasks, 'validated': True,
                        'first_batch_seconds': min(w['first_batch_at'] for w in workers)-start,
                        'peak_worker_rss_bytes': max(w['peak_rss_bytes'] for w in workers)})
                    self.save()
            self.validate_raw(self.expected)
            self.results['final_qpf'] = self.gate()
            self.results['status'] = 'complete'
            self.save()
        finally:
            with base.client(self.cfg) as client:
                assert client.get(base.MARKER) == self.owner.encode()
                for index in client.execute_command('FT._LIST'):
                    client.execute_command('FT.DROPINDEX', index)
                client.flushdb()
                assert not client.dbsize() and not client.execute_command('FT._LIST')


arrow_compare.Benchmark = Benchmark
if __name__ == '__main__':
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--suite', choices=['reads'], default='reads')
    parser.parse_known_args()
    if not any(arg == '--suite' or arg.startswith('--suite=') for arg in sys.argv[1:]):
        sys.argv.extend(('--suite', 'reads'))
    arrow_compare.main()
