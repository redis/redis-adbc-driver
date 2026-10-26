# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Run inside the CPU/memory-limited client container, using only stdlib."""
import argparse
import asyncio
import hashlib
import json
import os
import resource
from pathlib import Path

async def main(args):
    uri = args.uri
    proxy = None
    if args.delay_ms:
        proxy = await asyncio.create_subprocess_exec('/proxy', '-uri', uri, '-delay-ms', str(int(args.delay_ms)), stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
        uri = json.loads((await proxy.stdout.readline()).decode())['uri']
    env = dict(os.environ, GOMAXPROCS=str(args.cpu), REDIS_URI=uri,
               REDIS_WORKER_PROFILE_ROWS=str(args.rows), REDIS_WORKER_PROFILE_ROUNDS=str(args.rounds),
               REDIS_WORKER_PROFILE_WARMUP=str(args.warmup), REDIS_WORKER_PROFILE_REUSE='true',
               REDIS_WORKER_PROFILE_CEILING=args.workers, REDIS_WORKER_PROFILE_BUDGET=args.budget,
               REDIS_WORKER_PROFILE_MODE=args.mode)
    if args.mode == 'server-transition':
        env['REDIS_WORKER_PROFILE_TRANSITION_DIR'] = '/handoff'
    if args.concurrency:
        env['REDIS_WORKER_PROFILE_CONCURRENCY'] = str(args.concurrency)
    cgroup = {}
    for name in ['cpu.max', 'memory.max', 'cpu.stat', 'memory.current', 'memory.events']:
        path = Path('/sys/fs/cgroup') / name
        cgroup[name] = path.read_text() if path.exists() else None
    process = await asyncio.create_subprocess_exec('/profile', '-test.run=^TestAggregateWorkerConcurrencyProfile$',
        '-test.v', '-test.timeout=15m', env=env, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT)
    stdout, _ = await process.communicate()
    print(stdout.decode(), end='')
    records = []
    for line in stdout.decode().splitlines():
        if 'WORKER_PROFILE ' in line:
            records.append(json.loads(line.split('WORKER_PROFILE ', 1)[1]))
    proxy_metadata = {}
    if proxy:
        proxy.terminate()
        proxy_stdout, proxy_stderr = await proxy.communicate()
        if proxy.returncode or proxy_stderr:
            raise RuntimeError(proxy_stderr.decode())
        proxy_metadata = json.loads(proxy_stdout.decode())
    cgroup_after = {}
    for name in ['cpu.stat', 'memory.current', 'memory.events']:
        path = Path('/sys/fs/cgroup') / name
        cgroup_after[name] = path.read_text() if path.exists() else None
    usage = resource.getrusage(resource.RUSAGE_SELF)
    metadata = dict(cgroup=cgroup, cgroup_after=cgroup_after, orchestrator_process_cpu_seconds=usage.ru_utime + usage.ru_stime, proxy=proxy_metadata, proxy_delayed_responses=proxy_metadata.get('delayed_responses', 0),
                    proxy_errors=proxy_metadata.get('errors', 0), binary_sha256=hashlib.sha256(Path('/profile').read_bytes()).hexdigest())
    print('TRIAL_METADATA ' + json.dumps(metadata), flush=True)
    expected = 1 if args.concurrency else 3
    if process.returncode or len(records) != expected or any(r['GOMAXPROCS'] != args.cpu for r in records) or metadata['proxy_errors']:
        raise SystemExit(1)

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    for key in ['uri', 'workers', 'budget']:
        p.add_argument('--' + key, required=True)
    for key, default in [('cpu', 2), ('rows', 32768), ('rounds', 18), ('warmup', 60), ('concurrency', 0)]:
        p.add_argument('--' + key, type=int, default=default)
    p.add_argument('--delay-ms', type=float, default=0)
    p.add_argument('--mode', choices=['waves', 'continuous', 'server-transition'], default='waves')
    asyncio.run(main(p.parse_args()))
