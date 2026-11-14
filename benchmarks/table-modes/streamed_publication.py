"""Compare explicit managed publication units with bounded Arrow input batches."""

import argparse
import concurrent.futures
import json
import multiprocessing
from pathlib import Path
import resource
import subprocess
import sys
import time

import pyarrow as pa

import run as base


def write_worker(payload):
    cfg, lo, count, shape = payload
    table = base.source(cfg['source']).slice(lo, count)
    numbered = table.append_column(
        base.ORDINAL, pa.array(range(lo + 1, lo + count + 1), type=pa.int64())
    ).combine_chunks()
    batches = numbered.to_batches(max_chunksize=count if shape == 'one_batch' else 1000)
    with base.connect(cfg) as connection, connection.cursor() as cursor:
        if shape == 'many_calls':
            written = sum(cursor.adbc_ingest('managed', batch, mode='append', db_schema_name='bench')
                          for batch in batches)
            calls = len(batches)
        elif shape == 'one_stream':
            reader = pa.RecordBatchReader.from_batches(numbered.schema, iter(batches))
            try:
                written = cursor.adbc_ingest('managed', reader, mode='append', db_schema_name='bench')
            finally:
                reader.close()
            calls = 1
        else:
            assert shape == 'one_batch' and len(batches) == 1
            written = cursor.adbc_ingest('managed', batches[0], mode='append', db_schema_name='bench')
            calls = 1
        assert written == count
    return {'rows': written, 'calls': calls, 'input_batches': len(batches),
            'largest_input_batch': max(b.num_rows for b in batches),
            'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss *
            (1 if sys.platform == 'darwin' else 1024)}


class Benchmark(base.Benchmark):
    def run_writes(self):
        self.results['metadata'].update(
            scope='Explicit managed publication unit, same driver in every case',
            harness_source_commit=subprocess.check_output(
                ['git', 'rev-parse', 'HEAD'], cwd=Path(__file__).parent, text=True).strip(),
            automatic_retries=0, input_batch_rows=1000,
            full_input_materialized=True,
        )
        expected = self.expected.slice(0, self.args.write_rows)
        cases = [(shape, writers) for writers in (1, 4)
                 for shape in ('many_calls', 'one_stream', 'one_batch')]
        for repeat in range(-1, self.args.write_repeats):
            for shape, writers in (cases if repeat % 2 else reversed(cases)):
                self.reset()
                base.setup_managed(self.cfg, expected.slice(0, 0))
                count = self.args.write_rows // writers
                payloads = [(self.cfg, i * count, count, shape) for i in range(writers)]
                start = time.perf_counter()
                with concurrent.futures.ProcessPoolExecutor(
                    writers, mp_context=multiprocessing.get_context('spawn')
                ) as pool:
                    workers = list(pool.map(write_worker, payloads))
                elapsed = time.perf_counter() - start
                assert sum(w['rows'] for w in workers) == self.args.write_rows
                self.validate_sql('managed', expected)
                with base.connect(self.cfg) as connection, connection.cursor() as cursor:
                    cursor.execute('SELECT "' + base.ORDINAL + '" FROM bench.managed')
                    ids = cursor.fetch_arrow_table().column(0).to_pylist()
                    assert sorted(ids) == list(range(1, self.args.write_rows + 1))
                calls = sum(w['calls'] for w in workers)
                assert calls == (writers * ((count + 999) // 1000) if shape == 'many_calls' else writers)
                if shape != 'one_batch':
                    assert all(w['largest_input_batch'] <= 1000 for w in workers)
                row = {'shape': shape, 'mode': 'managed', 'api': 'arrow', 'writers': writers,
                       'repeat': repeat, 'warmup': repeat < 0, 'seconds': elapsed,
                       'rows': self.args.write_rows, 'columns': 105, 'validated': True,
                       'public_calls': calls, 'input_batches': sum(w['input_batches'] for w in workers),
                       'largest_input_batch': max(w['largest_input_batch'] for w in workers),
                       'peak_worker_rss_bytes': max(w['peak_rss_bytes'] for w in workers)}
                self.results['writes'].append(row)
                self.save()
                print('WRITE', json.dumps(row), flush=True)
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
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    parser.add_argument('--write-repeats', type=int, default=3)
    parser.set_defaults(write_rows=10000, read_repeats=0, sql_batch_rows=1000)
    args = parser.parse_args()
    if args.write_repeats < 1:
        parser.error('Require positive repetitions')
    benchmark = Benchmark(args)
    try:
        benchmark.run_writes()
    except Exception as error:
        benchmark.results['status'] = 'failed'
        benchmark.results['failures'].append({'error_type': type(error).__name__})
        benchmark.save()
        raise
    finally:
        benchmark.cleanup()


if __name__ == '__main__':
    main()
