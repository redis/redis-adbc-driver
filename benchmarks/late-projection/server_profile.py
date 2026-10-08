"""One-shot FT.PROFILE of first 1000 ordered rows; requires isolated fixture.

FT.PROFILE cannot use cursors. This instrumented pipeline substitutes LIMIT
0 1000 after the full sort and before loading. It does not measure client
transfer or prove a causal change in server sorting cost.
"""
import json
import os
from pathlib import Path
import redis

c=redis.Redis.from_url(os.environ['REDIS_URI'],protocol=2,decode_responses=True)
out={}
for label,load in [('wide',['LOAD',1,'@__key','LOAD','*']),
                   ('narrow',['LOAD',3,'@__key','@__rowid','@URL'])]:
    reply=c.execute_command('FT.PROFILE','idx:public:it_213_issue214','AGGREGATE',
        'LIMITED','QUERY','*','LOAD',3,'@EventTime','AS','__sort0','SORTBY',2,
        '@__sort0','ASC','MAX',100000,'LIMIT',0,1000,*load,'TIMEOUT',0,'DIALECT',2)
    out[label]=reply[-1]
Path('server-profile.json').write_text(json.dumps(out,indent=2)+'\n')
