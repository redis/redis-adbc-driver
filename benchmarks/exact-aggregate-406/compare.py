"""Read-only paired Q2/Q4/Q9/Q10 measurements on an independently loaded fixture.

Prefix runs use local subprocesses. Full runs require a ready recovery/restore
window, Linux cgroup isolation, a hard deadline and an ownership journal. Never
loads, repairs, reconfigures or deletes Redis data. Writes only a new work folder.
"""
import argparse
import importlib.metadata
import datetime
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import resource
import statistics
import subprocess
import sys
import threading
import time
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'benchmarks/clickbench'))
from diagnostics import error_details
from isolation import run_isolated, check_isolation, resolve_memory_limit

QUERIES = {
    2: 'SELECT COUNT(*) FROM hits WHERE AdvEngineID <> 0;',
    4: 'SELECT AVG(UserID) FROM hits;',
    9: 'SELECT RegionID, COUNT(DISTINCT UserID) AS u FROM hits GROUP BY RegionID ORDER BY u DESC LIMIT 10;',
    10: 'SELECT RegionID, SUM(AdvEngineID), COUNT(*) AS c, AVG(ResolutionWidth), COUNT(DISTINCT UserID) FROM hits GROUP BY RegionID ORDER BY c DESC LIMIT 10;',
}
SCHEMAS = {2:['int64'],4:['double'],9:['int32','int64'],10:['int32','int64','int64','double','int64']}
FULL_ROWS = 99997497
CUTOFF = datetime.datetime.fromisoformat('2026-10-22T06:55:00+00:00').timestamp()


def save(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(value, indent=2, default=str) + '\n')
    temporary.replace(path)


def sha(path):
    digest = hashlib.sha256()
    with open(path, 'rb') as source:
        for data in iter(lambda: source.read(8 << 20), b''):
            digest.update(data)
    return digest.hexdigest()


def worker(job_path):
    import adbc_driver_manager.dbapi as dbapi
    job = json.loads(Path(job_path).read_text())
    uri = json.load(sys.stdin)['uri']
    before = resource.getrusage(resource.RUSAGE_SELF)
    result = {'seconds': None, 'query': job['query'], 'sql': job['sql']}
    try:
        with dbapi.connect(driver=job['driver'], db_kwargs={
            'uri': uri, 'adbc.redis.aggregate_pushdown': 'exact',
            'adbc.redis.query_workers': str(job['workers']),
            'adbc.redis.query_worker_budget': str(job['workers']),
            'adbc.redis.read_timeout': '20m', 'adbc.redis.write_timeout': '20m',
        }, autocommit=True) as conn, conn.cursor() as cur:
            cur.execute('SET search_path TO ' + job['schema'])
            cur.adbc_statement.set_options(**{'adbc.redis.execution_diagnostics_file': job['diagnostics']})
            started = time.perf_counter()
            cur.execute(job['sql'])
            executed = time.perf_counter()
            table = cur.fetch_arrow_table()
            result.update(seconds=time.perf_counter() - started,
                          execute_seconds=executed-started,
                          fetch_seconds=time.perf_counter()-executed,
                          schema=[str(f.type) for f in table.schema],
                          values=[list(r) for r in zip(*(c.to_pylist() for c in table.columns))],
                          rows=table.num_rows,
                          worker_grant=int(conn.adbc_connection.get_option('adbc.redis.query_workers_last_grant')))
    except Exception as error:
        result.update(error_details(error, uri))
    after = resource.getrusage(resource.RUSAGE_SELF)
    result.update(client_user_cpu_seconds=after.ru_utime-before.ru_utime,
                  client_system_cpu_seconds=after.ru_stime-before.ru_stime,
                  process_peak_rss_bytes=after.ru_maxrss*(1 if sys.platform=='darwin' else 1024))
    path = Path(job['diagnostics'])
    if path.exists():
        result['execution_diagnostics'] = json.loads(path.read_text())
    save(job['output'], result)


def oracle(parquet, work):
    """Exact integer sums/counts, complete group universe and one final AVG round."""
    import duckdb
    con = duckdb.connect()
    con.execute("SET memory_limit='8GB'")
    con.execute("SET threads=4")
    con.execute('SET temp_directory=?', [str(work/'oracle-spill')])
    con.from_parquet(str(parquet)).create_view('hits')
    count, total, nonnull = con.execute('SELECT COUNT(*) FILTER(WHERE AdvEngineID<>0), SUM(CAST(UserID AS HUGEINT)), COUNT(UserID) FROM hits').fetchone()
    nine = {r[0]: [r[1]] for r in con.execute('SELECT RegionID, COUNT(DISTINCT UserID) FROM hits GROUP BY RegionID').fetchall()}
    ten = {}
    for key, adv, n, width, widths, users in con.execute('SELECT RegionID, SUM(CAST(AdvEngineID AS HUGEINT)), COUNT(*), SUM(CAST(ResolutionWidth AS HUGEINT)), COUNT(ResolutionWidth), COUNT(DISTINCT UserID) FROM hits GROUP BY RegionID').fetchall():
        ten[key] = [adv, n, float(Fraction(width, widths)) if widths else None, users]
    con.close()
    return {'2': [[count]], '4': [[float(Fraction(total, nonnull)) if nonnull else None]],
            '9': [[key, *values] for key, values in nine.items()],
            '10': [[key, *values] for key, values in ten.items()]}


def validate(number, actual, expected):
    def equal(a, b):
        if isinstance(b, float):
            # Exact integer accumulation, rounded once to the SQL double type.
            return isinstance(a, (int, float)) and float(a).hex() == b.hex()
        return a == b
    if number in (2, 4):
        return len(actual)==1 and len(actual[0])==1 and equal(actual[0][0], expected[0][0])
    groups = {row[0]: row[1:] for row in expected}
    if len(actual)!=min(10,len(groups)) or len({row[0] for row in actual})!=len(actual):
        return False
    # Q9 sorts distinct count; Q10 sorts COUNT(*). All tied boundary groups
    # are admissible only with exact complete values and the full rank sequence.
    column = 0 if number==9 else 1
    ranks = sorted((v[column] for v in groups.values()),reverse=True)[:len(actual)]
    for row, rank in zip(actual, ranks):
        if row[0] not in groups or len(row[1:])!=len(groups[row[0]]):
            return False
        if not all(equal(a,b) for a,b in zip(row[1:], groups[row[0]])) or row[column+1]!=rank:
            return False
    return True


class ServerStats:
    def __init__(self, uri):
        import redis
        seed=redis.Redis.from_url(uri, socket_timeout=2)
        try:
            clustered=seed.info('cluster').get('cluster_enabled')
            nodes={(seed.connection_pool.connection_kwargs['host'],seed.connection_pool.connection_kwargs['port'])}
            if clustered:
                nodes={(r[2][0].decode() if isinstance(r[2][0],bytes) else r[2][0],int(r[2][1])) for r in seed.execute_command('CLUSTER','SLOTS')}
        finally:
            seed.close()
        parts=urlsplit(uri)
        self.clients=[redis.Redis.from_url(urlunsplit(parts._replace(netloc=(parts.netloc.rsplit('@',1)[0]+'@' if '@' in parts.netloc else '')+host+':'+str(port))),socket_timeout=2) for host,port in sorted(nodes)]
        self.nodes=sorted(nodes)
        self.modules=[]
        for client in self.clients:
            try:
                self.modules.append([{'name':(m.get('name') or m.get(b'name')).decode() if isinstance(m.get('name') or m.get(b'name'),bytes) else m.get('name'), 'version':m.get('ver',m.get(b'ver'))} for m in client.module_list()])
            except Exception as error:
                self.modules.append({'unavailable':type(error).__name__})
        self.peak_rss=self.peak_used=0
        self.error=None
        self.done=threading.Event()
        self.thread=None

    def snapshot(self):
        records=[c.info() for c in self.clients]
        self.versions=[{'redis':r.get('redis_version'),'mode':r.get('redis_mode'),'os':r.get('os')} for r in records]
        return {'cpu_seconds':sum(r['used_cpu_user']+r['used_cpu_sys'] for r in records),
                'commands':sum(r['total_commands_processed'] for r in records),
                'request_bytes':sum(r['total_net_input_bytes'] for r in records),
                'reply_bytes':sum(r['total_net_output_bytes'] for r in records)}

    def sample(self):
        for client in self.clients:
            if self.done.is_set(): break
            r=client.info('memory')
            self.peak_rss=max(self.peak_rss,r['used_memory_rss'])
            self.peak_used=max(self.peak_used,r['used_memory'])

    def start(self):
        self.sample()
        def run():
            try:
                while not self.done.wait(.25):self.sample()
            except Exception as error:self.error=error_details(error)['error_type']
        self.thread=threading.Thread(target=run,daemon=True);self.thread.start()

    def stop(self):
        self.done.set()
        if self.thread:self.thread.join(5)
        if self.thread and self.thread.is_alive():raise RuntimeError('server monitor did not stop')

    def close(self):
        self.stop()
        for client in self.clients:client.close()


def check_full_window(gate, schema, deadline, now):
    if not deadline or not math.isfinite(deadline) or deadline>CUTOFF or deadline-now<300:
        raise ValueError('full experiment needs a bounded unexpired setup/timing window')
    if (gate.get('run')!='albert-clickbench-c6a-full-20261021' or
        gate.get('namespace')!=schema or gate.get('ready') is not True or gate.get('released',False) is not False or
        gate.get('backup_complete') is not True or gate.get('restore_tested') is not True):
        raise ValueError('full recovery, backup, restore and ownership gates are not satisfied')


def main():
    ap=argparse.ArgumentParser(description=__doc__)
    for n in ['baseline','candidate','parquet','work']:
        ap.add_argument('--'+n,type=Path,required=True)
    ap.add_argument('--schema',required=True)
    ap.add_argument('--provenance',type=Path,required=True)
    ap.add_argument('--rows',type=int,choices=[100000,FULL_ROWS],default=100000)
    ap.add_argument('--workers',type=int,default=4)
    ap.add_argument('--repeats',type=int,default=3)
    ap.add_argument('--timeout',type=int,default=1200)
    ap.add_argument('--memory-mib',type=int,default=16384)
    ap.add_argument('--full-gate',type=Path)
    ap.add_argument('--deadline',type=float)
    args=ap.parse_args()
    if not re.fullmatch(r'[a-zA-Z_][a-zA-Z0-9_]*',args.schema) or min(args.workers,args.repeats,args.timeout)<=0:
        ap.error('invalid schema/workers/repeats/timeout')
    full=args.rows==FULL_ROWS
    gate=None
    if full:
        if not args.full_gate:ap.error('full runs require a ready recovery/restore gate')
        gate=json.loads(args.full_gate.read_text())
        check_full_window(gate,args.schema,args.deadline,time.time())
        resolve_memory_limit(args.memory_mib)
        def record_preflight(unit):
            journal=json.loads(args.full_gate.read_text())
            journal.setdefault('active_job_units',[]).append(unit)
            journal.setdefault('preflight_job_units',[]).append(unit)
            save(args.full_gate,journal)
            print(json.dumps({'preflight_unit_started':unit}),flush=True)
        check_isolation(args.memory_mib,on_start=record_preflight)
    uri=os.environ['REDIS_URI']
    endpoint=urlsplit(uri);query=dict(parse_qsl(endpoint.query));query['protocol']='3'
    uri=urlunsplit(endpoint._replace(query=urlencode(query)))
    args.work.mkdir(parents=True,exist_ok=False)
    report={'status':'preparing','rows':args.rows,'workers':args.workers,'trials':[],
            'queries':QUERIES,'source_sha256':sha(args.parquet),
            'binaries':{n:{'sha256':sha(getattr(args,n))} for n in ['baseline','candidate']},
            'environment':{'platform':platform.platform(),'cpus':os.cpu_count(),'protocol':3,
                           'packages':{n:importlib.metadata.version(n) for n in ['pyarrow','redis','adbc-driver-manager','duckdb']}},
            'provenance':json.loads(args.provenance.read_text()),
            'scope':'ordinary statement; internal aggregate workers, not public ADBC partitions',
            'server_metric_scope':'INFO deltas include monitor/control traffic; sampled max primary RSS/used_memory, not a cgroup bound',
            'cpu_scope':'fresh worker process CPU, including connection/setup; elapsed query seconds include execute + complete Arrow fetch',
            'units':[]}
    output=args.work/'results.json';save(output,report)
    if full and report['source_sha256']!='a390f6cb782f6aaef278c72fc1dd86c4f30bc843ebab3c159e9bd4d45ddb079f':
        raise ValueError('full source checksum differs from the official preserved fixture')
    import pyarrow.parquet as pq
    if pq.ParquetFile(args.parquet).metadata.num_rows!=args.rows:raise ValueError('source file row count differs from requested immutable fixture')
    expected=oracle(args.parquet,args.work);save(args.work/'oracle.json',expected)
    stats=ServerStats(uri);report['primaries']=stats.nodes;report['modules_by_primary']=stats.modules
    if full and len(stats.nodes)!=48:raise ValueError('full comparison requires the recovered 48-primary topology')
    report['status']='running';save(output,report)
    def record_unit(unit):
        report['units'].append(unit);save(output,report)
        if full:
            journal=json.loads(args.full_gate.read_text())
            journal['job_units']=report['units']
            journal.setdefault('active_job_units',[]).append(unit)
            save(args.full_gate,journal)
        print(json.dumps({'unit_started':unit}),flush=True)
    try:
        for repeat in range(args.repeats):
            pairs=[(q,b) for q in QUERIES for b in ['baseline','candidate']]
            if repeat%2:pairs.reverse()
            for number,build in pairs:
                if full and time.time()>args.deadline-60:raise TimeoutError('full experiment cutoff reached')
                stem=f'q{number}-{build}-{repeat+1}'
                job={'query':number,'sql':QUERIES[number],'driver':str(getattr(args,build).resolve()),
                     'schema':args.schema,'workers':args.workers,
                     'output':str(args.work/(stem+'.json')),'diagnostics':str(args.work/(stem+'-diagnostics.json'))}
                path=args.work/(stem+'-job.json');save(path,job)
                command=[sys.executable,str(Path(__file__).resolve()),'--worker',str(path)]
                before=stats.snapshot();report['server_versions']=stats.versions;stats.peak_rss=stats.peak_used=0;stats.error=None;stats.done.clear();stats.start()
                try:
                    if full:
                        limit=min(args.timeout,max(1,int(args.deadline-time.time()-60)))
                        isolation=run_isolated(command,limit,args.memory_mib,credentials={'uri':uri},on_start=record_unit)
                    else:
                        worker_env={k:v for k,v in os.environ.items() if k!='REDIS_URI'}
                        completed=subprocess.run(command,input=json.dumps({'uri':uri}),text=True,capture_output=True,timeout=args.timeout,env=worker_env)
                        isolation={'exit_code':completed.returncode,'trial_isolation':'local process, no hard memory limit'}
                finally:stats.stop()
                after=stats.snapshot()
                result=json.loads(Path(job['output']).read_text()) if Path(job['output']).exists() else {'seconds':None,'error_type':'MissingWorkerResult'}
                result.update(build=build,trial=repeat+1,isolation=isolation,
                              server_delta={k:after[k]-before[k] for k in before},
                              max_primary_sampled_rss_bytes=stats.peak_rss,max_primary_sampled_used_bytes=stats.peak_used)
                result['valid']=result.get('seconds') is not None and not isolation.get('error_type') and not stats.error and result.get('schema')==SCHEMAS[number] and validate(number,result.get('values',[]),expected[str(number)])
                report['trials'].append(result);save(output,report)
                print(json.dumps({'query':number,'build':build,'trial':repeat+1,'seconds':result.get('seconds'),'valid':result['valid']}),flush=True)
                if not result['valid']:raise RuntimeError('invalid/failed trial; raw result retained')
        report['summary']=[]
        for q in QUERIES:
            for b in ['baseline','candidate']:
                trials=[r for r in report['trials'] if r['query']==q and r['build']==b]
                record={'query':q,'build':b,'trials':len(trials)}
                for key in ['seconds','client_user_cpu_seconds','client_system_cpu_seconds','process_peak_rss_bytes']:
                    record[key]=statistics.median(r[key] for r in trials)
                record['range_seconds']=[min(r['seconds'] for r in trials),max(r['seconds'] for r in trials)]
                report['summary'].append(record)
        report['status']='complete'
    except BaseException as error:
        report['status']='failed';report['failure']=error_details(error,uri);raise
    finally:
        stats.close();save(output,report)


if __name__=='__main__':
    if len(sys.argv)>1 and sys.argv[1]=='--worker':worker(sys.argv[2])
    else:main()
