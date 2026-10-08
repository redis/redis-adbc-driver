"""Reproducible, isolated Redis Software QPF benchmark (requires redis-py).

Run only against a dedicated disposable Redis Software cluster. Creates a
database and deterministic test data. The database stays until its container
is removed. Never points at an application database.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import json
import math
import pathlib
import ssl
import statistics
import threading
import time
import urllib.error
import urllib.request

import redis


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--rows', type=int, default=100000)
    ap.add_argument('--seconds', type=float, default=3)
    ap.add_argument('--output', default=str(pathlib.Path(__file__).parent))
    args = ap.parse_args()
    out = pathlib.Path(args.output)
    out.mkdir(parents=True, exist_ok=True)
    # Fixed test credentials; localhost-only dedicated disposable container.
    auth = base64.b64encode(b'bench@example.com:qpf-local-benchmark').decode()
    ctx = ssl._create_unverified_context()

    def api(path, body=None, method=None, authenticated=True):
        headers = {'Content-Type': 'application/json'}
        if authenticated:
            headers['Authorization'] = 'Basic ' + auth
        req = urllib.request.Request('https://127.0.0.1:19443/v1/' + path,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, context=ctx, timeout=20) as reply:
                data = reply.read()
                return json.loads(data) if data else None
        except urllib.error.HTTPError as e:
            raise RuntimeError(f'{path}: HTTP {e.code}: {e.read().decode()}') from e

    def wait(check, label):
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            if check():
                return
            time.sleep(1)
        raise RuntimeError('Timed out: ' + label)

    try:
        bootstrap = api('bootstrap')
    except RuntimeError:
        bootstrap = api('bootstrap', authenticated=False)
    if bootstrap['bootstrap_status']['state'] == 'idle':
        api('bootstrap/create_cluster', {
            'action': 'create_cluster', 'cluster': {'name': 'qpf-bench.local'},
            'node': {},
            'credentials': {'username': 'bench@example.com', 'password': 'qpf-local-benchmark'}
        }, authenticated=False)
    wait(lambda: api('bootstrap')['bootstrap_status']['state'] == 'completed', 'bootstrap')
    bdbs = api('bdbs')
    matching = [b for b in bdbs if b['name'] == 'qpf-benchmark']
    if matching:
        bdb = matching[0]
    else:
        for attempt in range(30):
            try:
                bdb = api('bdbs', {
                    'name': 'qpf-benchmark', 'type': 'redis', 'redis_version': '8.6',
                    'memory_size': 1024 * 1024 * 1024, 'port': 12100,
                    'replication': False, 'data_persistence': 'disabled',
                    'eviction_policy': 'noeviction',
                })
                break
            except RuntimeError as e:
                if 'HTTP 406' not in str(e) or attempt == 29:
                    raise
                time.sleep(2)
    uid = bdb['uid']
    wait(lambda: api(f'bdbs/{uid}')['status'] == 'active', 'database')
    client = redis.Redis(host='127.0.0.1', port=12100, socket_timeout=60, protocol=2)
    client.ping()
    meta = {'rows': args.rows, 'seconds_per_trial': args.seconds, 'rounds': 3,
            'concurrency': [1, 16], 'server': client.info('server')['redis_version'],
            'modules': client.execute_command('MODULE', 'LIST'), 'configs': []}
    if b'qpf_idx' not in client.execute_command('FT._LIST'):
        client.execute_command('FT.CREATE', 'qpf_idx', 'ON', 'HASH', 'PREFIX', 1, 'qpf:',
                               'SCHEMA', 'text', 'TEXT', 'NOSTEM', 'country', 'TAG', 'SORTABLE',
                               'amount', 'NUMERIC', 'SORTABLE', 'amount_raw', 'NUMERIC',
                               'id', 'NUMERIC', 'SORTABLE')
        pipe = client.pipeline(transaction=False)
        for i in range(args.rows):
            pipe.hset(f'qpf:{i}', mapping={
                'text': ['alpha beta', 'alpha gamma', 'delta beta', 'delta epsilon'][i % 4],
                'country': ['US', 'UK', 'DE', 'JP', 'FR', 'CA'][i % 6],
                'amount': i % 1000, 'amount_raw': i % 1000, 'id': i,
                'payload': 'x' * 512,
            })
            if i % 1000 == 999:
                pipe.execute()
        pipe.execute()
    def index_info():
        values = client.execute_command('FT.INFO', 'qpf_idx')
        return dict(zip(values[::2], values[1::2]))
    wait(lambda: int(index_info()[b'num_docs']) == args.rows and
         int(index_info()[b'indexing']) == 0, 'indexing')
    meta['index_docs'] = int(index_info()[b'num_docs'])
    queries = {
        'search_text_count': ['FT.SEARCH', 'qpf_idx', 'alpha|beta', 'NOCONTENT', 'LIMIT', 0, 0, 'DIALECT', 2],
        'search_tag_projection': ['FT.SEARCH', 'qpf_idx', '@country:{US}', 'SORTBY', 'id', 'ASC',
                                  'RETURN', 2, 'id', 'amount', 'LIMIT', 0, 10, 'DIALECT', 2],
        'search_full_document': ['FT.SEARCH', 'qpf_idx', '@country:{US}', 'SORTBY', 'id', 'ASC',
                                 'LIMIT', 0, 100, 'DIALECT', 2],
        'aggregate_sortable': ['FT.AGGREGATE', 'qpf_idx', '*', 'GROUPBY', 1, '@country',
                               'REDUCE', 'SUM', 1, '@amount', 'AS', 'total',
                               'SORTBY', 2, '@country', 'ASC', 'DIALECT', 2],
        'aggregate_load': ['FT.AGGREGATE', 'qpf_idx', '*', 'LOAD', 1, '@amount_raw',
                           'GROUPBY', 1, '@country', 'REDUCE', 'SUM', 1, '@amount_raw', 'AS', 'total',
                           'SORTBY', 2, '@country', 'ASC', 'DIALECT', 2],
        'search_numeric_count': ['FT.SEARCH', 'qpf_idx', '@amount:[100 110]', 'NOCONTENT',
                                 'LIMIT', 0, 0, 'DIALECT', 2],
    }
    meta['queries'] = queries
    expected = {name: client.execute_command(*q) for name, q in queries.items()}
    assert expected['aggregate_sortable'] == expected['aggregate_load']
    meta['result_hashes'] = {name: hashlib.sha256(repr(v).encode()).hexdigest() for name, v in expected.items()}
    records = []
    def write():
        (out / 'results.json').write_text(json.dumps({'metadata': meta, 'trials': records}, indent=2, default=str))

    for round_idx, factors in enumerate([[0, 2, 4], [4, 0, 2], [2, 4, 0]], 1):
        for factor in factors:
            # active=false retains the previous effective settings on this image.
            requested = {'active': True, 'scaling_factor': factor}
            api(f'bdbs/{uid}', {'query_performance_factor': requested}, 'PUT')
            wait(lambda: api(f'bdbs/{uid}')['status'] == 'active', 'QPF update')
            time.sleep(2)
            observed = api(f'bdbs/{uid}?fields=query_performance_factor,sched_policy,conns,search,module_list')
            effective = {k: observed.get(k) for k in ['query_performance_factor', 'sched_policy', 'conns', 'search', 'module_list']}
            meta['configs'].append({'round': round_idx, 'factor': factor, 'effective': effective})
            if (effective['query_performance_factor'] != requested
                    or effective['search']['search-workers'] != math.ceil(factor * 1.5)
                    or effective['sched_policy'] != ('mnp' if factor else 'cmp')
                    or effective['conns'] != (32 if factor else 5)):
                raise RuntimeError(f'QPF not applied: {effective}')
            for name, query in queries.items():
                assert client.execute_command(*query) == expected[name], f'Result mismatch: {name}'
            for concurrency in [1, 16]:
                for name, query in queries.items():
                    start = [0.0]
                    def begin():
                        start[0] = time.perf_counter()
                    barrier = threading.Barrier(concurrency + 1, action=begin)
                    def worker():
                        conn = redis.Redis(host='127.0.0.1', port=12100, socket_timeout=60, protocol=2)
                        for _ in range(3):
                            conn.execute_command(*query)
                        latencies, errors = [], []
                        barrier.wait()
                        while time.perf_counter() < start[0] + args.seconds:
                            t0 = time.perf_counter()
                            try:
                                conn.execute_command(*query)
                                latencies.append((time.perf_counter() - t0) * 1000)
                            except Exception as e:
                                errors.append(str(e))
                        conn.close()
                        return latencies, errors
                    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
                        tasks = [pool.submit(worker) for _ in range(concurrency)]
                        barrier.wait()
                        # Start duration after all clients have connected and warmed up.
                        elapsed_start = start[0]
                        results = [t.result() for t in tasks]
                        elapsed = time.perf_counter() - elapsed_start
                    samples = sorted(x for latencies, _ in results for x in latencies)
                    errors = [x for _, err in results for x in err]
                    assert samples and not errors, errors
                    record = {'round': round_idx, 'qpf': factor, 'concurrency': concurrency,
                              'query': name, 'requests': len(samples), 'elapsed_s': elapsed,
                              'qps': len(samples) / elapsed, 'p50_ms': statistics.median(samples),
                              'p95_ms': samples[math.ceil(.95 * len(samples)) - 1], 'errors': len(errors)}
                    records.append(record)
                    print(json.dumps(record), flush=True)
                    write()
    # Restore standard QPF in the dedicated benchmark database.
    api(f'bdbs/{uid}', {'query_performance_factor': {'active': True, 'scaling_factor': 0}}, 'PUT')
    write()


if __name__ == '__main__':
    main()
