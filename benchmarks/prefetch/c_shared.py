#!/usr/bin/env python3
"""Matched complete-fetch C shared-library consumer; command JSON arrives on stdin.

Used by screen.py with private drivers configuration. Independent validation is
performed by the parent after this process exits. No value/type coercion occurs.
Go allocation counters are unavailable through this boundary and remain null.
"""
import concurrent.futures
import contextlib
import json
from pathlib import Path
import resource
import sys
import time

import adbc_driver_manager.dbapi as adbc
import pyarrow as pa


def run(command):
    with contextlib.ExitStack() as stack:
        connections = [stack.enter_context(adbc.connect(
            driver=command['Driver'], autocommit=True,
            db_kwargs={'uri': command['URI'], 'adbc.redis.tls_ca_file': command['CA'],
                       'adbc.redis.stream_batch_rows': str(command['BatchRows'])}))
                       for _ in range(command['Readers'])]
        pool = stack.enter_context(concurrent.futures.ThreadPoolExecutor(max_workers=command['Readers']))
        started_cpu = resource.getrusage(resource.RUSAGE_SELF)
        started = time.perf_counter()
        if command['API'] == 'partition':
            planner = stack.enter_context(connections[0].cursor())
            planner.adbc_statement.set_options(**{'adbc.redis.partition_count': 4})
            descriptors, _ = planner.adbc_execute_partitions(command['SQL'])
            tasks = list(descriptors)
        else:
            tasks = command['Queries']
        metrics = [None] * len(tasks)

        def consume(worker):
            for task in range(worker, len(tasks), command['Readers']):
                with connections[worker].cursor() as cursor:
                    if command['API'] == 'partition':
                        cursor.adbc_read_partition(tasks[task])
                    else:
                        cursor.execute(tasks[task])
                    batches = cursor.fetch_record_batch()
                    rows, count, first_rows, first_seconds = 0, 0, 0, None
                    try:
                        with pa.OSFile(str(Path(command['Output'])/f'part-{task:03d}.arrow'), 'wb') as sink:
                            with pa.ipc.new_stream(sink, batches.schema) as writer:
                                for batch in batches:
                                    if count == 0:
                                        first_rows = batch.num_rows
                                        first_seconds = time.perf_counter()-started
                                    writer.write_batch(batch)
                                    rows += batch.num_rows
                                    count += 1
                    finally:
                        batches.close()
                    metrics[task] = {'Rows': rows, 'Batches': count,
                                     'FirstRows': first_rows, 'FirstSeconds': first_seconds}

        futures = [pool.submit(consume, worker) for worker in range(command['Readers'])]
        for future in futures:
            future.result()
        if command['API'] == 'partition':
            planner.close()
        seconds = time.perf_counter()-started
        usage = resource.getrusage(resource.RUSAGE_SELF)
        return {'seconds': seconds,
                'client_cpu_seconds': usage.ru_utime+usage.ru_stime-started_cpu.ru_utime-started_cpu.ru_stime,
                'metrics': metrics, 'api': command['API'], 'readers': command['Readers'],
                'batch_rows': command['BatchRows'],
                'allocated_bytes': None, 'allocations': None, 'sampled_peak_go_heap_bytes': None,
                'process_lifetime_peak_rss_bytes': usage.ru_maxrss*(1 if sys.platform == 'darwin' else 1024),
                'allocator': 'Python/PyArrow and C shared library; Go allocation counters unavailable',
                'timing': 'public C ADBC execute/partition planning, complete fetch, IPC output and cursor cleanup; connections preopened; validation excluded'}


if __name__ == '__main__':
    try:
        result = run(json.load(sys.stdin))
    except Exception as error:
        print(json.dumps({'error_type': type(error).__name__, 'error': str(error)}))
        sys.exit(1)
    print(json.dumps(result))
