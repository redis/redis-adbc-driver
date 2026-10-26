"""Optional full profiles, only after all primary pairs complete exactly."""
import json
import math
from pathlib import Path
import statistics
import sys
import time


def run_profiles(config, uri, module):
    primary=json.loads((Path(config['work'])/'results.json').read_text())
    if primary['status']!='complete' or len(primary['trials'])!=24 or not all(t['valid'] for t in primary['trials']):
        raise RuntimeError('optional profiles require all 24 exact primary trials')
    expected=json.loads((Path(config['work'])/'oracle.json').read_text())
    directory=Path(config['profile_work']);directory.mkdir(exist_ok=False)
    report={'status':'running','scope':'Go Arrow helper; native C Arrow primary timings are separate',
            'profiles':[],'not_run':[],'deadline':config['deadline']}
    output=directory/'results.json';module.save(output,report)
    gate_path=Path(config['gate'])
    def record_unit(unit):
        gate=json.loads(gate_path.read_text())
        gate.setdefault('active_job_units',[]).append(unit)
        gate.setdefault('profile_job_units',[]).append(unit)
        module.save(gate_path,gate)
        print(json.dumps({'profile_unit_started':unit}),flush=True)
    try:
        for number in [10,4,9,2]:
            estimated=sum(statistics.median(t['seconds'] for t in primary['trials'] if t['query']==number and t['build']==b) for b in ['baseline','candidate'])
            # Profiles are optional: leave room for their measured overhead,
            # service cleanup, preservation and operator verification.
            if config['deadline']-time.time() < estimated*1.5+300:
                report['not_run'].append({'query':number,'reason':'insufficient profile window','estimated_pair_seconds':estimated})
                module.save(output,report);continue
            for build in ['baseline','candidate']:
                module.check_full_window(json.loads(gate_path.read_text()),config['schema'],config['deadline'],time.time())
                stem=directory/(build+'-q'+str(number))
                timeout=min(1200,math.floor(config['deadline']-time.time()-180))
                job={'query':number,'schema':config['schema'],'workers':16,
                     'binary':config[build+'_profile_binary'],'stem':str(stem),
                     'timeout':timeout-10,'output':str(stem)+'-result.json'}
                path=Path(str(stem)+'-job.json');module.save(path,job)
                isolation=module.run_isolated([sys.executable,config['profile_worker'],str(path)],timeout,16384,
                                             credentials={'uri':uri},on_start=record_unit)
                result=json.loads(Path(job['output']).read_text()) if Path(job['output']).exists() else {'status':'failed','error_type':'MissingProfileResult'}
                result.update(build=build,isolation=isolation)
                valid=False
                if result['status']=='complete' and not isolation.get('error_type'):
                    profile=result['profile'];q=profile['query']
                    values=[[None if v=='NULL' else float(v) if q==4 or q==10 and i==3 else int(v)
                             for i,v in enumerate(row)] for row in profile['values']]
                    valid=module.validate(q,values,expected[str(q)])
                result['valid']=valid;report['profiles'].append(result);module.save(output,report)
                print(json.dumps({'profile_query':number,'build':build,'valid':valid}),flush=True)
                if not valid:raise RuntimeError('failed or invalid profile; raw evidence retained')
        report['status']='complete' if not report['not_run'] else 'partial'
    except BaseException as error:
        report['status']='failed';report['failure']=module.error_details(error,uri);raise
    finally:
        module.save(output,report)
