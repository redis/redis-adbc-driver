"""Supervisor regressions: a stub coordinator and synthetic engines, no Redis or SF1 queries."""

import argparse
from contextlib import redirect_stdout
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import isolation


HERE = Path(__file__).resolve().parent

# Stand-in for run.py. Loading exports the query files; a refused query exits
# the way run.py's recheck_flex_compaction does, before run_query writes a
# checkpoint; any other query runs the real isolation.run_query with engine
# workers that report success.
STUB = '''
import argparse, json, subprocess, sys
from pathlib import Path
sys.path.insert(0, HARNESS)  # the real isolation.py and run.py
import isolation

p = argparse.ArgumentParser()
p.add_argument("suite")
p.add_argument("--output", type=Path)
p.add_argument("--private", type=Path)
p.add_argument("--include-duckdb-adbc", action="store_true")
p.add_argument("--load-only", action="store_true")
p.add_argument("--reuse-loaded", action="store_true")
p.add_argument("--query")
args = p.parse_args()
folder = args.output / args.suite
if args.load_only:
    (folder / "queries").mkdir(parents=True)
    for n in QUERIES:
        (folder / "queries" / f"{n}.sql").write_text("SELECT synthetic")
    sys.exit()
if args.query in REFUSE:
    with (folder / "events.jsonl").open("a") as out:
        out.write(json.dumps({"event": "flex_compaction_refused", "query": args.query}) + "\\n")
    raise SystemExit(f"refusing to time query {args.query}: synthetic uncompacted index")


def runner(cmd, **kwargs):
    engine = cmd[cmd.index("--engine") + 1]
    if cmd[cmd.index("--phase") + 1] == "engine":
        artifacts = Path(cmd[cmd.index("--artifacts") + 1])
        (artifacts / f"{engine}.json").write_text(json.dumps(
            {f"{engine}_status": "executed", f"{engine}_median_seconds": 1.0}))
    return subprocess.CompletedProcess(cmd, 0)


args.duckdb_only = False
isolation.run_query(args, runner=runner)
'''

MEMORY_EVENTS = "Path('/sys/fs/cgroup/memory.events')"


class SupervisorTests(unittest.TestCase):
    def test_coordinator_refused_before_checkpoint_does_not_poison_later_queries(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            harness = root / 'harness'
            harness.mkdir()
            # supervise.py starts the run.py beside it. Point its client OOM
            # counter at a synthetic cgroup file; nothing else changes.
            source = (HERE / 'supervise.py').read_text()
            self.assertEqual(source.count(MEMORY_EVENTS), 1)
            (harness / 'supervise.py').write_text(
                source.replace(MEMORY_EVENTS, "Path(os.environ['SYNTHETIC_MEMORY_EVENTS'])"))
            (harness / 'run.py').write_text(
                f"HARNESS = {str(HERE)!r}\nQUERIES = ['1', '2', '3']\nREFUSE = {{'1'}}\n" + STUB)
            events = root / 'memory.events'
            events.write_text('oom 0\noom_kill 0\n')
            private = root / 'private'
            private.mkdir()
            output = root / 'results'

            result = subprocess.run(
                [sys.executable, str(harness / 'supervise.py'), 'tpch',
                 '--output', str(output), '--private', str(private)],
                capture_output=True, text=True, timeout=120,
                env={**os.environ, 'SYNTHETIC_MEMORY_EVENTS': str(events)})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('refusing to time query 1', result.stderr)
            self.assertNotIn('historical shared-worker results', result.stderr)

            folder = output / 'tpch'
            lines = (folder / 'queries.jsonl').read_text().splitlines()
            records = {r['query']: r for r in map(json.loads, lines)}
            self.assertEqual(len(lines), 3)
            self.assertEqual(sorted(records), ['1', '2', '3'])
            refused = records['1']
            self.assertEqual(refused['coordinator_error'], 'coordinator exited with code 1')
            self.assertEqual(refused['failed_phase'], 'coordinator')
            self.assertEqual(refused['client_oom_kills_delta'], 0)
            self.assertGreaterEqual(refused['worker_wall_seconds'], 0)
            self.assertEqual(set(refused), {'suite', 'query', 'engine_isolation', 'coordinator_error',
                                            'failed_phase', 'worker_wall_seconds',
                                            'client_oom_kills_delta'})
            for n in ('2', '3'):
                self.assertNotIn('coordinator_error', records[n])
                self.assertEqual(records[n]['duckdb_status'], 'ok')
                self.assertEqual(records[n]['redis_status'], 'ok')
                # The fallback carries the value run_query itself writes.
                self.assertEqual(refused['engine_isolation'], records[n]['engine_isolation'])
            self.assertFalse(list(folder.glob('checkpoint-*.json')))
            refusals = [json.loads(x) for x in (folder / 'events.jsonl').read_text().splitlines()]
            self.assertEqual(refusals[0], {'event': 'flex_compaction_refused', 'query': '1'})
            self.assertEqual(refusals[-1], {'event': 'complete', 'suite': 'tpch'})

            # A genuinely historical shared-worker record is still refused.
            with (folder / 'queries.jsonl').open('a') as out:
                out.write(json.dumps({'suite': 'tpch', 'query': '9'}) + '\n')
            args = argparse.Namespace(private=private, output=output, suite='tpch', query='4',
                                      duckdb_only=False, include_duckdb_adbc=False)
            with redirect_stdout(io.StringIO()), self.assertRaisesRegex(
                    ValueError, 'historical shared-worker results must remain separate'):
                isolation.run_query(args, runner=lambda cmd, **kwargs: self.fail('engine started'))


if __name__ == '__main__':
    unittest.main()
