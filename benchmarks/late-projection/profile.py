"""Run stage workers (go test -c) compiled from both driver versions."""
import argparse
import json
import os
from pathlib import Path
import subprocess

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--baseline',type=Path,required=True)
p.add_argument('--worker',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
results=[]
for trial in range(3):
    workers=[('baseline',a.baseline),('late',a.worker)]
    if trial%2: workers.reverse()
    for label,worker in workers:
        for case,pattern in [('q24','google'),('matching','produkty'),('rare','gaw.ru'),('none','issue214AbsentSentinel')]:
            sql=f"SELECT * FROM it_213_issue214 WHERE URL LIKE '%{pattern}%' ORDER BY EventTime LIMIT 10"
            env=dict(os.environ,REDIS_LATE_PROFILE_QUERY=sql)
            output=subprocess.check_output([str(worker.resolve()),'-test.run=^TestLateProjectionProfile$','-test.v'],env=env,text=True)
            lines=[line.split('PROFILE ',1)[1] for line in output.splitlines() if 'PROFILE ' in line]
            result=json.loads(lines[0]); result.update(strategy=label,case=case,trial=trial+1)
            results.append(result)
            print(json.dumps(result),flush=True)
            a.output.write_text(json.dumps(results,indent=2)+'\n')
