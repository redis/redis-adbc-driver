"""Alternating complete-fetch screen using an independently typed Arrow source.

Private config is read from a file, never copied into the report. This tool
does not load data or provision infrastructure. Verify the restored Software
database's effective 4x QPF before invocation and between measured profiles.
"""
import argparse
import hashlib
import itertools
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.parse

import pyarrow as pa
from meter import Meter


def canonical(table):
    return table.sort_by([(name, 'ascending') for name in table.column_names]).combine_chunks()


def validate(paths, expected):
    actual = pa.concat_tables([pa.ipc.open_stream(str(p)).read_all() for p in paths])
    if not canonical(actual).equals(canonical(expected), check_metadata=False):
        raise AssertionError('Independent full-value/type/multiplicity mismatch')


def query(namespace, columns, lo, hi):
    def quote(s):
        return '"' + s.replace('"', '""') + '"'
    return ('SELECT ' + ','.join(map(quote, columns)) + ' FROM ' + quote(namespace)
            + '.hits WHERE __rowid>=' + str(lo) + ' AND __rowid<=' + str(hi))


def trial(cfg, source, build, api, readers, projection, repeat, root):
    import redis
    probe = subprocess.run(cfg['qpf_probe'], capture_output=True, text=True, timeout=120, check=True)
    proof = json.loads(probe.stdout)
    if not (proof.get('verified') and proof.get('factor') == 4 and proof.get('search_workers') == 6
            and proof.get('sched_policy') == 'mnp' and proof.get('conns') == 32):
        raise AssertionError('Effective Software 4x QPF gate failed')
    selected = source if projection == 'wide' else source.select(['watchid', 'userid', 'searchphrase'])
    target = root / f'{projection}-{api}-{readers}-{build}-{repeat}'
    target.mkdir()  # Never overwrite a previous attempt, including a failure.
    sql = query(cfg['namespace'], selected.column_names, 1, 100000)
    queries = [query(cfg['namespace'], selected.column_names, i*100000//readers+1,
                     (i+1)*100000//readers) for i in range(readers)]
    command = {'URI': cfg['uri'], 'CA': cfg['ca'], 'API': api, 'SQL': sql,
               'Queries': queries, 'Output': str(target), 'Readers': readers,
               'BatchRows': cfg.get('batch_rows', 65536)}
    with redis.Redis.from_url(cfg['uri'], ssl_ca_certs=cfg['ca'], protocol=3) as control:
        before = control.info()
        meter = None
        wire = None
        try:
            if cfg.get('wire_diagnostic'):
                endpoint = urllib.parse.urlsplit(cfg['uri'])
                meter = Meter((endpoint.hostname, endpoint.port or 6379))
                netloc = '127.0.0.1:' + str(meter.port)
                if '@' in endpoint.netloc:
                    netloc = endpoint.netloc.rsplit('@', 1)[0] + '@' + netloc
                command['URI'] = urllib.parse.urlunsplit(endpoint._replace(netloc=netloc))
            started = time.monotonic()
            process = subprocess.run([cfg['binaries'][build]], input=json.dumps(command),
                                     text=True, capture_output=True, timeout=1200)
            process_seconds = time.monotonic() - started
        finally:
            if meter is not None:
                wire = meter.close()
        after = control.info()
    (target/'stdout.json').write_text(process.stdout)
    (target/'stderr.txt').write_text(process.stderr)
    if process.returncode:
        raise RuntimeError(f'{build}/{api}/{readers}: harness exit {process.returncode}; raw failure retained')
    result = json.loads(process.stdout)
    paths = sorted(target.glob('*.arrow'))
    if not paths:
        raise AssertionError('No complete-fetch output')
    validate(paths, selected)  # Outside harness and process timing.
    result.update(build=build, projection=projection, repeat=repeat, warmup=repeat < 0,
                  process_seconds=process_seconds, independent_full_values_validated=True,
                  rows=selected.num_rows, columns=selected.num_columns,
                  qpf_proof=proof,
                  server_counter_scope='INFO on the Software endpoint; includes control-query overhead; not a per-shard CPU decomposition')
    if wire is not None:
        result.update(request_bytes=wire[0], reply_bytes=wire[1],
                      wire_scope='TCP payload read from benchmark-client and server relay legs; includes TLS framing/handshake, excludes IP headers and separate INFO control; diagnostic timings only')
    for name in ['used_cpu_user', 'used_cpu_sys', 'total_net_input_bytes', 'total_net_output_bytes']:
        if name in after and name in before:
            result[name+'_delta'] = after[name] - before[name]
    return result


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--config', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--repeats', type=int, default=5)
    ap.add_argument('--smoke', action='store_true')
    ap.add_argument('--wire-diagnostic', action='store_true')
    args = ap.parse_args()
    if args.repeats < 5 and not args.smoke:
        ap.error('Promotion screen requires at least five measured repeats')
    cfg = json.loads(args.config.read_text())
    cfg['wire_diagnostic'] = args.wire_diagnostic
    if not cfg.get('verified_software_qpf_4x') or not cfg.get('qpf_probe'):
        ap.error('A verified active Software 4x QPF declaration is required')
    source_path = Path(cfg['source'])
    source = pa.ipc.open_file(str(source_path)).read_all()
    if source.num_rows != 100000 or source.num_columns != 105:
        ap.error('Expected independently typed 100K x 105 source')
    if hashlib.sha256(source_path.read_bytes()).hexdigest() != cfg['source_sha256']:
        ap.error('Independent source identity differs')
    args.output.mkdir(parents=True, exist_ok=False)
    report = {'status': 'running', 'smoke': args.smoke, 'wire_diagnostic':args.wire_diagnostic, 'trials': [],
              'source_sha256': cfg['source_sha256'], 'source_provenance': cfg['source_provenance'],
              'binaries': {name: hashlib.sha256(Path(path).read_bytes()).hexdigest()
                           for name, path in cfg['binaries'].items()},
              'environment': {'logical_cpus': os.cpu_count(), 'batch_rows': cfg.get('batch_rows', 65536),
                              'protocol': 3, 'tls': 'verified', 'software_qpf': 4}}
    output = args.output/'results.json'
    def save():
        tmp = output.with_suffix('.tmp')
        tmp.write_text(json.dumps(report, indent=2))
        tmp.replace(output)
    save()
    try:
        for repeat in range(-1, args.repeats):
            cases = list(itertools.product(['wide', 'narrow'], ['regular', 'partition'], [1, 4]))
            if repeat % 2 == 0:
                cases.reverse()
            for projection, api, readers in cases:
                for build in (['baseline', 'candidate'] if repeat % 2 else ['candidate', 'baseline']):
                    result = trial(cfg, source, build, api, readers, projection, repeat, args.output)
                    report['trials'].append(result)
                    save()
                    print(json.dumps({k: result[k] for k in ['build', 'projection', 'api', 'readers', 'repeat', 'seconds']}), flush=True)
    except Exception as error:
        report.update(status='failed', failure_type=type(error).__name__, failure=str(error))
        save()
        raise
    report['status'] = 'passed'
    save()


if __name__ == '__main__':
    main()
