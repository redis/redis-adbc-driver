#!/usr/bin/env python3
"""Run against an isolated Redis (only it_worker_profile is modified).
Build first: cd go && go test -c -o /tmp/redis-tuner.test .
Example: python3 benchmarks/worker-autotune/matrix.py --binary /tmp/redis-tuner.test --uri redis://127.0.0.1:16379/0 --output /tmp/worker-results.jsonl
The impaired link adds 2 ms before forwarding each server-read chunk; this is
not a precise RTT emulator. Keep other Redis and client activity idle.
"""
import argparse
import asyncio
import json
import os
import subprocess
import threading
from urllib.parse import urlsplit


def proxy(host, port, ready):
    async def connection(reader, writer):
        upstream_reader, upstream_writer = await asyncio.open_connection(host, port)
        async def relay(source, target, delay):
            try:
                while data := await source.read(65536):
                    if delay:
                        await asyncio.sleep(delay)
                    target.write(data)
                    await target.drain()
            finally:
                target.close()
        await asyncio.gather(relay(reader, upstream_writer, 0), relay(upstream_reader, writer, .002), return_exceptions=True)
    async def serve():
        server = await asyncio.start_server(connection, '127.0.0.1', 0)
        ready.append(server.sockets[0].getsockname()[1])
        async with server:
            await server.serve_forever()
    asyncio.run(serve())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--uri', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--rows', type=int, default=16384)
    parser.add_argument('--rounds', type=int, default=30)
    args = parser.parse_args()
    uri = urlsplit(args.uri)
    if uri.scheme != 'redis' or uri.username or uri.password:
        parser.error('use an isolated, unauthenticated local redis:// endpoint')
    ready = []
    threading.Thread(target=proxy, args=(uri.hostname, uri.port or 6379, ready), daemon=True).start()
    import time
    while not ready:
        time.sleep(.01)
    with open(args.output, 'w') as output:
        for condition, endpoint in [('direct', args.uri), ('impaired', f'redis://127.0.0.1:{ready[0]}{uri.path}')]:
            for cpu in [1, 2, 4]:
                for policy, workers, budget in [('serial', '1', str(cpu)), ('fixed', str(cpu*2), str(cpu*2)), ('tuned', 'auto', 'auto')]:
                    env = dict(os.environ, GOMAXPROCS=str(cpu), REDIS_URI=endpoint,
                               REDIS_WORKER_PROFILE_ROWS=str(args.rows), REDIS_WORKER_PROFILE_ROUNDS=str(args.rounds),
                               REDIS_WORKER_PROFILE_CEILING=workers, REDIS_WORKER_PROFILE_BUDGET=budget)
                    run = subprocess.run([args.binary, '-test.run=^TestAggregateWorkerConcurrencyProfile$', '-test.v', '-test.timeout=5m'], env=env, capture_output=True, text=True, check=True)
                    for line in run.stdout.splitlines():
                        if 'WORKER_PROFILE ' in line:
                            record = json.loads(line.split('WORKER_PROFILE ', 1)[1])
                            record.update(condition=condition, policy=policy)
                            output.write(json.dumps(record) + '\n')
                            output.flush()
                    print(f'{condition} CPU={cpu} {policy} complete', flush=True)

if __name__ == '__main__':
    main()
