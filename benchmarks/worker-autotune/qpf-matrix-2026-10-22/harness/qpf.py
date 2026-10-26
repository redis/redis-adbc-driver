#!/usr/bin/env python3
"""Focused QPF policy comparison on an already isolated Software database.

Uses the existing Go correctness/performance profiler and readiness thresholds.
Only its named fixture is written. --configure-qpf changes the selected database.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import random
import ssl
import subprocess
import time
import urllib.request

from gate import POLICIES, evaluate, write_report


def verify_qpf(database, factor=4):
    if not (database.get('status') == 'active'
            and database.get('query_performance_factor') == {'active': True, 'scaling_factor': factor}
            and database.get('search', {}).get('search-workers') == factor * 3 // 2
            and database.get('sched_policy') == ('mnp' if factor else 'cmp')
            and database.get('conns') == (32 if factor else 5)):
        raise RuntimeError(f'QPF {factor} effective configuration not verified')
    return database


def command(argv):
    return subprocess.check_output(argv, text=True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binary', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--network', required=True)
    p.add_argument('--server-container', required=True)
    p.add_argument('--uri', required=True, help='private Docker endpoint')
    p.add_argument('--api-port', type=int, required=True, help='localhost Software management port')
    p.add_argument('--database', default='adbc-test')
    p.add_argument('--image', default='redis-adbc-linux-builder:1.26.10-arm64')
    p.add_argument('--cpu', type=int, choices=[1, 2, 4], default=2)
    p.add_argument('--rows', type=int, default=32768)
    p.add_argument('--warmup', type=int, default=60)
    p.add_argument('--rounds', type=int, default=18)
    p.add_argument('--repetitions', type=int, default=3)
    p.add_argument('--qpf', type=int, choices=[0, 2, 4, 8], default=4, help='0 means Standard, active=true')
    p.add_argument('--configure-qpf', action='store_true', help='set QPF on this disposable database before timing')
    p.add_argument('--modes', nargs='+', choices=['waves', 'continuous'], default=['waves', 'continuous'])
    args = p.parse_args()
    if args.repetitions < 3 or args.rows < 2048 or min(args.warmup, args.rounds) < 1:
        p.error('require >=3 repetitions, >=2048 rows and positive phase lengths')
    args.output.mkdir(parents=True, exist_ok=False)
    binary = args.binary.resolve()
    auth = base64.b64encode((os.getenv('SOFTWARE_ADMIN_USER', 'admin@example.com') + ':' +
                            os.getenv('SOFTWARE_ADMIN_PASSWORD', 'adbc-software-test')).encode()).decode()

    def api(path, body=None):
        request = urllib.request.Request(f'https://localhost:{args.api_port}/v1/{path}',
                                         data=json.dumps(body).encode() if body is not None else None,
                                         method='PUT' if body is not None else 'GET',
                                         headers={'Authorization': 'Basic ' + auth, 'Content-Type': 'application/json'})
        with urllib.request.urlopen(request, context=ssl._create_unverified_context(), timeout=30) as response:
            return json.load(response)

    uid = next(b['uid'] for b in api('bdbs') if b['name'] == args.database)
    def configuration():
        database = verify_qpf(api(f'bdbs/{uid}?fields=uid,name,version,status,query_performance_factor,search,sched_policy,conns,shards_count,module_list'), args.qpf)
        return {k: database.get(k) for k in ['uid', 'name', 'version', 'status', 'query_performance_factor',
                                            'search', 'sched_policy', 'conns', 'shards_count', 'module_list']}

    if args.configure_qpf:
        api(f'bdbs/{uid}', {'query_performance_factor': {'active': True, 'scaling_factor': args.qpf}})
        deadline = time.monotonic() + 180
        while True:
            try:
                configuration()
                break
            except RuntimeError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(1)
        time.sleep(2)
    scenarios = [dict(deployment='software', rows=args.rows, cpu=args.cpu, condition='direct',
                      mode=mode, concurrencies=[1, 2, 4]) for mode in args.modes]
    rng = random.Random(226286)
    schedule = []
    for repetition in range(args.repetitions):
        block = [dict(s, repetition=repetition, policy=policy) for s in scenarios for policy in POLICIES]
        rng.shuffle(block)
        schedule.extend(block)
    manifest = dict(scenarios=scenarios, schedule=schedule, repetitions=args.repetitions,
                    bounds=dict(steady_qps_min=.90, steady_p95_max=1.15, cold_qps_min=.85, cold_p95_max=1.20),
                    qpf=configuration(), source_commit=command(['git', 'rev-parse', 'HEAD']).strip(),
                    source_diff_sha256=hashlib.sha256(command(['git', 'diff']).encode()).hexdigest(),
                    binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                    runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                    image=json.loads(command(['docker', 'image', 'inspect', args.image])),
                    server=json.loads(command(['docker', 'inspect', args.server_container])),
                    activity_before=command(['docker', 'ps', '--format', '{{json .}}']),
                    cpu=args.cpu, memory_bytes=2 * 1024**3, warmup=args.warmup, rounds=args.rounds,
                    scope=f'One aggregate query, QPF held at {args.qpf}, single Software shard, direct connection; not full readiness.')
    if not manifest['server'][0]['Config']['Image'].startswith('redislabs/redis:'):
        raise RuntimeError('server must be Redis Software')
    (args.output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    records, errors = [], []
    try:
        for number, spec in enumerate(schedule):
            before = configuration()
            workers, budget = {'serial': ('1', str(args.cpu)), 'cpu-fixed': (str(args.cpu), str(args.cpu)),
                               'headroom-fixed': (str(2 * args.cpu), str(2 * args.cpu)), 'tuned': ('auto', 'auto')}[spec['policy']]
            env = dict(GOMAXPROCS=str(args.cpu), REDIS_URI=args.uri, REDIS_WORKER_PROFILE_ROWS=str(args.rows),
                       REDIS_WORKER_PROFILE_REUSE='true', REDIS_WORKER_PROFILE_WARMUP=str(args.warmup),
                       REDIS_WORKER_PROFILE_ROUNDS=str(args.rounds), REDIS_WORKER_PROFILE_MODE=spec['mode'],
                       REDIS_WORKER_PROFILE_CEILING=workers, REDIS_WORKER_PROFILE_BUDGET=budget)
            argv = ['docker', 'run', '--rm', '--network', args.network, '--cpus', str(args.cpu),
                    '--memory', '2g', '--memory-swap', '2g', '--mount', f'type=bind,src={binary},dst=/profile,readonly']
            for key, value in env.items():
                argv += ['-e', key + '=' + value]
            argv += ['--entrypoint', '/profile', args.image, '-test.v', '-test.timeout=10m',
                     '-test.run=^TestAggregateWorkerConcurrencyProfile$']
            result = subprocess.run(argv, text=True, capture_output=True, timeout=660)
            (args.output / f'trial-{number:02d}.log').write_text(result.stdout + result.stderr)
            after = configuration()
            data = [json.loads(line.split('WORKER_PROFILE ', 1)[1]) for line in result.stdout.splitlines() if 'WORKER_PROFILE ' in line]
            if result.returncode or len(data) != 3 or {r['concurrency'] for r in data} != {1, 2, 4}:
                raise RuntimeError(f'trial {number} failed; see log')
            for record in data:
                record.update(spec, qpf_before=before, qpf_after=after)
            records.extend(data)
            (args.output / 'results.json').write_text(json.dumps(records) + '\n')
            print(f'{number + 1}/{len(schedule)} {spec["mode"]} {spec["policy"]}', flush=True)
    except Exception as error:
        errors.append(str(error))
    result = evaluate(records, manifest)
    if errors:
        result['status'] = 'infrastructure'
        result['infrastructure'].extend(errors)
    write_report(args.output, result)
    print('Gate: ' + result['status'], flush=True)
    raise SystemExit({'pass': 0, 'regression': 2, 'infrastructure': 1}[result['status']])


if __name__ == '__main__':
    main()
