"""Capture FT.PROFILE loader-stage times for a sample of an existing fixture.

Requires redis-py. REDIS_URI must point to an isolated benchmark server.
"""
import argparse
import json
import os
from pathlib import Path

import redis

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--index', required=True)
p.add_argument('--rows', type=int, default=1000)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
client = redis.Redis.from_url(os.environ['REDIS_URI'], protocol=2, decode_responses=True)
result = {}
for label, load in [('load_all', ['LOAD', 1, '@__key', 'LOAD', '*']),
                    ('narrow', ['LOAD', 2, '@__key', '@__rowid'])]:
    reply = client.execute_command('FT.PROFILE', a.index, 'AGGREGATE', 'LIMITED',
                                   'QUERY', '*', 'LIMIT', 0, a.rows, *load,
                                   'TIMEOUT', 0, 'DIALECT', 2)
    result[label] = reply[-1]
a.output.write_text(json.dumps(result, indent=2)+'\n')
print(json.dumps(result))
