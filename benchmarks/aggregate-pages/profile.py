"""Run fresh-process Go allocation/heap workers on an existing fixture.

Copy aggregate_pages_profile_test.go into the baseline checkout and compile
both repositories with `go test -c`. REDIS_URI selects the profiling server.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--baseline', type=Path, required=True)
p.add_argument('--worker', type=Path, required=True)
p.add_argument('--queries', type=Path, required=True)
p.add_argument('--table', default='it_212_hits')
p.add_argument('--rows', type=int, default=100000)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
if not re.fullmatch(r'it_212_[a-z0-9_]+', a.table):
    p.error('use a dedicated it_212_ table')
if 'REDIS_URI' not in os.environ:
    p.error('set REDIS_URI to the dedicated benchmark server')
queries = a.queries.read_text().splitlines()
results = []
for trial in range(3):
    variants = [('baseline', a.baseline), ('incremental', a.worker)]
    if trial % 2: variants.reverse()
    for strategy, binary in variants:
        for query in (9, 17, 33):
            env = dict(os.environ, REDIS_AGG_PROFILE_QUERY=re.sub(r'\bhits\b', a.table, queries[query-1]))
            text = subprocess.check_output([str(binary.resolve()), '-test.run=^TestAggregatePagesProfile$', '-test.v'], env=env, text=True)
            match = re.search(r'PROFILE (\{.*\})', text)
            if not match: raise RuntimeError(text)
            result = json.loads(match[1])
            result.update(strategy=strategy, query_id=query, input_rows=a.rows, trial=trial+1)
            results.append(result)
            a.output.write_text(json.dumps(results, indent=2)+'\n')
            print(json.dumps(result), flush=True)
