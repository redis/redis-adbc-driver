import argparse, json, statistics
from pathlib import Path
import benchmark as b
original=b.native_redis_raw_batches

def discovery(client, fields, start, stop):
    index='idx:transferbench:hits'
    query='*' if (start,stop)==(1,100000) else f'@__rowid:[{start} {stop}]'
    cursor=0
    try:
        reply=client.execute_command('FT.AGGREGATE',index,query,'LOAD',2,'@__key','@__rowid','TIMEOUT',0,'WITHCURSOR','COUNT',1869,'DIALECT',2)
        while True:
            page,cursor=reply
            keys=[dict(zip(row[::2],row[1::2]))[b'__key'] for row in page[1:]]
            for offset in range(0,len(keys),1000):
                with client.pipeline(transaction=False) as pipe:
                    for key in keys[offset:offset+1000]:pipe.hmget(key,['__rowid']+fields)
                    rows=pipe.execute()
                if any(row[0] is None for row in rows):raise AssertionError('Fixture row deleted')
                yield [row[1:] for row in rows]
            if not cursor:break
            reply=client.execute_command('FT.CURSOR','READ',index,cursor,'COUNT',1869)
    finally:
        if cursor:client.execute_command('FT.CURSOR','DEL',index,cursor)

def main():
    root=Path(__file__).resolve().parent
    source=b.pa.ipc.open_file(str(root/'source.arrow')).read_all()
    expected=source.sort_by([(n,'ascending') for n in source.column_names]).combine_chunks()
    args=argparse.Namespace(driver=str(root/'main.dylib'),work=root/'discovery')
    args.work.mkdir(exist_ok=True)
    (args.work/'source.arrow').write_bytes((root/'source.arrow').read_bytes())
    pairs=[(p,m) for p in ['single_reader','four_readers'] for m in ['known_keys','search_discovery']]
    report={'trials':[],'status':'running'}
    # Spawn imports this module as __mp_main__, so the generator selection is
    # propagated through the environment and installed at module import below.
    import os
    for repeat in [-1,0,1,2]:
        shift=(repeat+1)%len(pairs)
        for profile,mode in pairs[shift:]+pairs[:shift]:
            os.environ['TRANSFER_DISCOVERY_MODE']=mode
            record=b.trial(args,'redis_py',profile,repeat,expected)
            record['discovery_mode']=mode
            report['trials'].append(record)
            (root/'discovery-results.json').write_text(json.dumps(report,indent=2))
    report['summary']=[]
    for profile,mode in pairs:
        rows=[r for r in report['trials'] if not r['warmup'] and (r['profile'],r['discovery_mode'])==(profile,mode)]
        report['summary'].append({'profile':profile,'mode':mode,'median_seconds':statistics.median(r['seconds'] for r in rows),'min_seconds':min(r['seconds'] for r in rows),'max_seconds':max(r['seconds'] for r in rows)})
    report['status']='complete'
    (root/'discovery-results.json').write_text(json.dumps(report,indent=2))
    print(json.dumps(report['summary'],indent=2),flush=True)
import os
if os.environ.get('TRANSFER_DISCOVERY_MODE')=='search_discovery':b.native_redis_raw_batches=discovery
if __name__=='__main__':main()
