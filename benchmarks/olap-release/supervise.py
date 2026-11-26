"""Isolate queries so a released native driver's crash cannot hide coverage."""

import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


p = argparse.ArgumentParser()
p.add_argument("suite", choices=["ssb", "tpch", "tpcds"])
p.add_argument("--reuse-loaded", action="store_true")
p.add_argument("--output", type=Path)
p.add_argument("--private", type=Path, default=Path("/private"))
p.add_argument("--include-duckdb-adbc", action="store_true")
args = p.parse_args()
root = Path(__file__).resolve().parent
output = args.output or (root / "results")
folder = output / args.suite
cmd = [sys.executable, "-u", str(root / "run.py"), args.suite, "--output", str(output),
       "--private", str(args.private)]
if args.include_duckdb_adbc:
    cmd.append("--include-duckdb-adbc")
loading = cmd + ["--load-only"] + (["--reuse-loaded"] if args.reuse_loaded else [])
subprocess.run(loading, check=True)
query_files = sorted((folder / "queries").glob("*.sql"), key=lambda p: [int(n) for n in p.stem.split('.')])
def oom_kills():
    values = dict(line.split() for line in Path('/sys/fs/cgroup/memory.events').read_text().splitlines())
    return int(values['oom_kill'])

for query_file in query_files:
    n = query_file.stem
    print(json.dumps({"event": "query_start", "suite": args.suite, "query": n}), flush=True)
    start = time.perf_counter()
    oom_before = oom_kills()
    try:
        # The coordinator bounds each engine and validator separately. This
        # outer ceiling includes all three optional engine/validation pairs.
        with subprocess.Popen(cmd + ["--reuse-loaded", "--query", n], start_new_session=True) as worker:
            try:
                code = worker.wait(timeout=1200)
            except BaseException:
                # The process group includes the coordinator's engine/validator
                # child. Stopping a supervisor must not leave queries running.
                os.killpg(worker.pid, signal.SIGKILL)
                worker.wait()
                raise
        failure = f"coordinator exited with code {code}" if code else None
    except subprocess.TimeoutExpired:
        failure = "query coordinator exceeded 1200-second process limit"
    if failure:
        checkpoint = folder / f"checkpoint-{n}.json"
        # A coordinator can exit before run_query writes a checkpoint (e.g. a
        # Flex compaction refusal). Mark the fallback as a current-harness
        # record, or every later run_query refuses it as historical output.
        record = json.loads(checkpoint.read_text()) if checkpoint.exists() else {
            "suite": args.suite, "query": n,
            "engine_isolation": "sequential fresh processes; separate disk-backed validation"}
        engine = record.get("active_engine")
        phase = record.get("active_phase", "coordinator")
        if engine:
            record[f"{engine}_status"] = "validation_failed" if phase == "validation" else "worker_failed"
            record[f"{engine}_error"] = failure
        record.update(coordinator_error=failure, failed_phase=phase,
                      worker_wall_seconds=time.perf_counter()-start,
                      client_oom_kills_delta=oom_kills()-oom_before)
        with (folder / "queries.jsonl").open("a") as out:
            out.write(json.dumps(record) + "\n")
        print(json.dumps(record), flush=True)
        checkpoint.unlink(missing_ok=True)
with (folder / "events.jsonl").open("a") as out:
    out.write(json.dumps({"event": "complete", "suite": args.suite}) + "\n")
print(json.dumps({"event": "complete", "suite": args.suite}), flush=True)
