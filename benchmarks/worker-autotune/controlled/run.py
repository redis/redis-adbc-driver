#!/usr/bin/env python3
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

"""Isolated Docker matrix; write the manifest BEFORE running measurements.
Exit 0=gate passed, 2=performance regression, 1=infrastructure failure,
130=operator interruption (partial evidence is preserved).
Servers must already be isolated, seeded only by this runner, and idle.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import random
import subprocess
import time
from gate import POLICIES, evaluate, write_report

ROOT = Path(__file__).resolve().parent

def command(args):
    return subprocess.run(args, check=True, text=True, capture_output=True).stdout

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binary', required=True)
    p.add_argument('--proxy-binary', required=True)
    p.add_argument('--baseline-binary', help='optional old policy with the identical new profiler; diagnostic OSS/CPU4 comparisons')
    p.add_argument('--network', required=True)
    p.add_argument('--server', action='append', required=True, help='name=redis://private-host:port/0')
    p.add_argument('--server-container', action='append', required=True, help='Docker names matching --server order')
    p.add_argument('--output', required=True)
    p.add_argument('--image', default='python:3.14-slim')
    p.add_argument('--cpus', default='1,2,4')
    p.add_argument('--rows', type=int, default=32768)
    p.add_argument('--large-rows', type=int, default=100000)
    p.add_argument('--repetitions', type=int, default=3)
    p.add_argument('--warmup', type=int, default=60)
    p.add_argument('--rounds', type=int, default=18)
    p.add_argument('--seed', type=int, default=226286)
    args = p.parse_args()
    output, binary = Path(args.output).resolve(), Path(args.binary).resolve()
    proxy_binary = Path(args.proxy_binary).resolve()
    if output.exists():
        p.error('output must be new: preserve the predeclared manifest and previous trials')
    if args.server and any(s.split('=', 1)[0] not in ['oss', 'software'] for s in args.server):
        p.error('deployment names must be oss or software')
    if args.repetitions < 3 or args.warmup < 1 or args.rounds < 1:
        p.error('at least three repetitions and positive phase lengths required')
    output.mkdir(parents=True)
    (output / 'logs').mkdir()
    if not args.cpus or any(c not in ['1', '2', '4'] for c in args.cpus.split(',')):
        p.error('CPU quotas must be selected from 1,2,4')
    servers = dict(s.split('=', 1) for s in args.server)
    if len(servers) != len(args.server_container):
        p.error('one isolated server container required per deployment')
    scenarios = [dict(deployment=d, rows=args.rows, cpu=c, condition=link, mode='waves', concurrencies=[1, 2, 4])
                 for d in servers for c in map(int, args.cpus.split(',')) for link in ['direct', 'aggregate-response-2ms']]
    # Larger data and continuous arrivals are independent spot checks; report
    # their coverage precisely rather than implying a full second factorial.
    scenarios += [dict(deployment=d, rows=args.large_rows, cpu=2, condition='direct', mode=mode, concurrencies=[1, 2, 4])
                  for d in servers for mode in ['waves', 'continuous']]
    scenarios += [dict(deployment=d, rows=args.rows, cpu=2, condition='direct', mode='server-transition', concurrencies=[1, 2, 4]) for d in servers]
    rng = random.Random(args.seed)
    schedule = []
    for repetition in range(args.repetitions):
        blocks = scenarios.copy()
        rng.shuffle(blocks)
        for scenario in blocks:
            policies = POLICIES.copy()
            if args.baseline_binary and scenario['deployment'] == 'oss' and scenario['cpu'] == 4:
                policies.append('legacy-tuned')
            rng.shuffle(policies)
            schedule.extend(dict(scenario, repetition=repetition, policy=policy) for policy in policies)
    manifest = dict(created_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        baseline_comparison=bool(args.baseline_binary), baseline_binary_sha256=hashlib.sha256(Path(args.baseline_binary).read_bytes()).hexdigest() if args.baseline_binary else None,
        objective='End-to-end steady cohort throughput with a same-policy p95 guard; separate cold-learning guard.',
        bounds=dict(steady_qps_min=.90, steady_p95_max=1.15, cold_qps_min=.85, cold_p95_max=1.20),
        harness_sha256={p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in ROOT.glob('*.py')},
        host_load_average=os.getloadavg(),
        seed=args.seed, repetitions=args.repetitions, warmup_rounds=args.warmup, measured_rounds=args.rounds,
        scenarios=scenarios, schedule=schedule, server_uris=servers,
        delay_definition='2 ms once per complete FT.AGGREGATE or FT.CURSOR response, after upstream completion; other commands and TCP chunks are not delayed. Not network RTT.',
        client_memory_bytes=2 * 1024**3,
        phase_definition='Cold learning excludes fixture creation and the serial reference check. Fresh handle per trial, one handle across concurrency phases. Warmed windows may still contain probes. Server-transition measurements include adaptation to the changed quota.', client_image=json.loads(command(['docker', 'image', 'inspect', args.image])),
        server_containers=json.loads(command(['docker', 'inspect', *args.server_container])),
        docker_info=json.loads(command(['docker', 'info', '--format', '{{json .}}'])),
        activity_before=command(['docker', 'ps', '--format', '{{json .}}']),
        stats_before=command(['docker', 'stats', '--no-stream', '--format', '{{json .}}']),
        source_commit=command(['git', 'rev-parse', 'HEAD']).strip(),
        source_diff_sha256=hashlib.sha256(command(['git', 'diff']).encode()).hexdigest(),
        binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
        proxy_binary_sha256=hashlib.sha256(proxy_binary.read_bytes()).hexdigest())
    architecture = {'aarch64': 'arm64', 'x86_64': 'amd64'}.get(manifest['docker_info']['Architecture'], manifest['docker_info']['Architecture'])
    if manifest['client_image'][0]['Architecture'] != architecture:
        p.error('validation client image must be native to the Docker host')
    (output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    def trial(spec, seed=False):
        cpu, policy = spec['cpu'], spec['policy']
        workers, budget = {'serial': ('1', str(cpu)), 'cpu-fixed': (str(cpu), str(cpu)),
                           'headroom-fixed': (str(cpu * 2), str(cpu * 2)), 'tuned': ('auto', 'auto'), 'legacy-tuned': ('auto', 'auto')}[policy]
        trial_binary = Path(args.baseline_binary).resolve() if policy == 'legacy-tuned' else binary
        inner = ['python', '/runner/trial.py', '--uri', servers[spec['deployment']], '--cpu', str(cpu),
                 '--workers', workers, '--budget', budget, '--rows', str(spec['rows']), '--mode', spec['mode'],
                 '--warmup', str(0 if seed else args.warmup), '--rounds', str(1 if seed else args.rounds)]
        if seed:
            inner += ['--concurrency', '1']
        if not seed and spec['condition'] != 'direct':
            inner += ['--delay-ms', '2']
        handoff = output / 'handoff'
        handoff.mkdir(exist_ok=True)
        for name in ['request', 'ack']:
            (handoff / name).unlink(missing_ok=True)
        container = args.server_container[list(servers).index(spec['deployment'])]
        events = []
        client_name = f'autotune-client-{time.time_ns()}'
        client = subprocess.Popen(['docker', 'run', '--rm', '--name', client_name, '--network', args.network, '--cpus', str(cpu),
            '--memory', '2g', '--memory-swap', '2g', '--mount', f'type=bind,src={trial_binary},dst=/profile,readonly',
            '--mount', f'type=bind,src={proxy_binary},dst=/proxy,readonly',
            '--mount', f'type=bind,src={ROOT},dst=/runner,readonly',
            '--mount', f'type=bind,src={handoff},dst=/handoff', args.image, *inner], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        # communicate() drains logs while the handshake watcher changes server
        # quotas. It cannot wait on stdout buffers or guess a phase from time.
        import threading
        stopped = threading.Event()
        failures = []
        def watch():
            seen = None
            while not stopped.wait(.02):
                request = handoff / 'request'
                if not request.exists(): continue
                try:
                    token = request.read_text()
                    if token == seen: continue
                    _, quota = token.split(':')
                    if quota not in ['1', '2']: raise ValueError('unexpected quota')
                    command(['docker', 'update', '--cpus', quota, container])
                    events.append(dict(token=token, cpu=int(quota), container=container))
                    (handoff / 'ack').write_text(token)
                    seen = token
                except Exception as error:
                    failures.append(str(error))
                    return
        watcher = threading.Thread(target=watch, daemon=True)
        watcher.start()
        try:
            stdout, stderr = client.communicate(timeout=1000)
        finally:
            if client.poll() is None:
                subprocess.run(['docker', 'rm', '-f', client_name], capture_output=True)
                client.kill()
                client.communicate()
            stopped.set()
            watcher.join()
            if spec['mode'] == 'server-transition': command(['docker', 'update', '--cpus', '2', container])
        if failures: raise RuntimeError(str(failures))
        records, metadata = [], None
        for line in stdout.splitlines():
            if 'WORKER_PROFILE ' in line:
                records.append(json.loads(line.split('WORKER_PROFILE ', 1)[1]))
            if line.startswith('TRIAL_METADATA '):
                metadata = json.loads(line.split(' ', 1)[1])
        if client.returncode or metadata is None or len(records) != (1 if seed else 3):
            raise RuntimeError(stdout + stderr)
        # Confirm real cgroup limits, rather than labelling a GOMAXPROCS-only run
        # as a CPU-quota measurement.
        quota, period = metadata['cgroup']['cpu.max'].split()
        if int(quota) / int(period) != cpu or int(metadata['cgroup']['memory.max']) != 2 * 1024**3:
            raise RuntimeError('client cgroup quota differs from predeclared limits')
        for record in records:
            record.update(spec, trial_metadata=metadata, server_quota_events=events)
        return records, stdout + stderr
    records, errors, cancelled = [], [], False
    try:
        for deployment in servers:
            for rows in sorted({s['rows'] for s in scenarios}):
                _, log = trial(dict(deployment=deployment, rows=rows, cpu=2, condition='direct', mode='waves', policy='serial'), seed=True)
                (output / 'logs' / f'seed-{deployment}-{rows}.txt').write_text(log)
        with gzip.open(output / 'results.jsonl.gz', 'wt') as stream:
            for number, spec in enumerate(schedule):
                started = time.monotonic()
                try:
                    data, log = trial(spec)
                    (output / 'logs' / f'{number:03d}.txt').write_text(log)
                    for record in data:
                        stream.write(json.dumps(record) + '\n')
                    stream.flush()
                    records.extend(data)
                    print(f"{number + 1}/{len(schedule)} {spec} {time.monotonic() - started:.1f}s", flush=True)
                except Exception as error:
                    errors.append(dict(trial=spec, error=str(error)))
                    (output / 'logs' / f'{number:03d}-error.txt').write_text(str(error))
                    print(f"Infrastructure failure in trial {number}: {error}", flush=True)
    except KeyboardInterrupt:
        cancelled = True
    except Exception as error:
        errors.append(dict(error=str(error)))
    manifest['stats_after'] = command(['docker', 'stats', '--no-stream', '--format', '{{json .}}'])
    manifest['activity_after'] = command(['docker', 'ps', '--format', '{{json .}}'])
    # Completion metadata is separate so the predeclared manifest remains immutable.
    (output / 'completion.json').write_text(json.dumps(dict(cancelled=cancelled, completed_records=len(records), planned_trials=len(schedule), activity_after=manifest['activity_after'], stats_after=manifest['stats_after'], errors=errors), indent=2) + '\n')
    result = evaluate(records, manifest)
    if errors:
        result['status'] = 'infrastructure'
        result['infrastructure'].extend(errors)
    if cancelled:
        result['status'] = 'cancelled'
        result['note'] = 'Stopped by the operator. Missing trials are incomplete coverage, not measured Redis failures. No readiness conclusion. ' + result['note']
    write_report(output, result)
    print(f"Gate: {result['status']}; {sum(c['passed'] for c in result['cases'])}/{len(result['cases'])} cases pass", flush=True)
    raise SystemExit({'pass': 0, 'regression': 2, 'infrastructure': 1, 'cancelled': 130}[result['status']])

if __name__ == '__main__':
    main()
