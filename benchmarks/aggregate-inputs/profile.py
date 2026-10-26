"""Profile production allocations using the existing generic aggregate worker."""
import argparse
import json
import os
from pathlib import Path
import subprocess
from run import query

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--worker',type=Path,required=True)
p.add_argument('--baseline',type=Path,required=True)
p.add_argument('--queries',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
results=[]
for trial in range(3):
    workers=[('baseline',a.baseline),('typed',a.worker)]
    if trial%2:workers.reverse()
    for label,worker in workers:
        for width in [1,10,90]:
            env=dict(os.environ,REDIS_AGG_PROFILE_QUERY=query(width,a.queries))
            output=subprocess.check_output([str(worker.resolve()),'-test.run=^TestExactDistinctProfile$','-test.v'],text=True,env=env)
            lines=[line.split('PROFILE ',1)[1] for line in output.splitlines() if 'PROFILE ' in line]
            result=json.loads(lines[0]);result.update(strategy=label,width=width,trial=trial+1)
            results.append(result);print(json.dumps(result),flush=True)
            a.output.write_text(json.dumps(results,indent=2)+'\n')
