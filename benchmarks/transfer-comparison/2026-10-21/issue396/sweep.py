import argparse, json, os, statistics
from pathlib import Path
import benchmark as b

def main():
    root=Path(__file__).resolve().parent
    source=b.pa.ipc.open_file(str(root/'source.arrow')).read_all()
    expected=source.sort_by([(n,'ascending') for n in source.column_names]).combine_chunks()
    args=argparse.Namespace(driver=str(root/'main.dylib'),work=root/'sweep')
    args.work.mkdir(exist_ok=True)
    (args.work/'source.arrow').write_bytes((root/'source.arrow').read_bytes())
    pairs=[('redis',p,size) for p in ['single_reader','four_readers'] for size in [65536,8192,2048]]
    pairs += [('redis_py',p,65536) for p in ['single_reader','four_readers']]
    report={'commit':'20de26967b9974fa344f15de2b71491039321d86','trials':[],'status':'running'}
    for repeat in [-1,0,1,2]:
        shift=(repeat+1)*3%len(pairs)
        for engine,profile,size in pairs[shift:]+pairs[:shift]:
            os.environ['TRANSFER_BATCH_ROWS']=str(size)
            record=b.trial(args,engine,profile,repeat,expected)
            record['stream_batch_rows']=size
            report['trials'].append(record)
            (root/'sweep-results.json').write_text(json.dumps(report,indent=2))
    report['summary']=[]
    for engine,profile,size in pairs:
        rows=[r for r in report['trials'] if not r['warmup'] and (r['engine'],r['profile'],r['stream_batch_rows'])==(engine,profile,size)]
        report['summary'].append({'engine':engine,'profile':profile,'batch_rows':size,
           'median_seconds':statistics.median(r['seconds'] for r in rows),
           'min_seconds':min(r['seconds'] for r in rows),'max_seconds':max(r['seconds'] for r in rows),
           'median_first_batch_seconds':statistics.median(r['time_to_first_batch_seconds'] for r in rows),
           'median_max_worker_peak_rss_bytes':statistics.median(max(m['worker_lifetime_peak_rss_bytes'] for m in r['metrics']) for r in rows)})
    report['status']='complete'
    (root/'sweep-results.json').write_text(json.dumps(report,indent=2))
    print(json.dumps(report['summary'],indent=2),flush=True)
if __name__=='__main__':main()
