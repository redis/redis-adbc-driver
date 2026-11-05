#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Run an opt-in Flex profile with 20ms numeric server process sampling.

Set REDIS_URI, REDIS_FLEX_BENCH_DIR, REDIS_FLEX_BENCH_TARGET and
REDIS_FLEX_BENCH_CONTAINER for an owned Software fixture. Select
TestFlexComparativeProfile (default) or TestFlexColdResourceProfile with
REDIS_FLEX_BENCH_TEST. Query timings exclude sampler startup/teardown.
No container identifiers are written to the numeric timeline.
"""

import os
from pathlib import Path
import re
import subprocess
import sys
import time


def main() -> int:
    root = Path(__file__).resolve().parents[1]
    output = Path(os.environ["REDIS_FLEX_BENCH_DIR"])
    target = os.environ["REDIS_FLEX_BENCH_TARGET"]
    test = os.environ.get("REDIS_FLEX_BENCH_TEST", "^TestFlexComparativeProfile$")
    if test not in {"^TestFlexComparativeProfile$", "^TestFlexColdResourceProfile$"}:
        raise SystemExit("select one of the two documented opt-in Flex profiles")
    if target not in {"ram", "flex"}:
        raise SystemExit("REDIS_FLEX_BENCH_TARGET must be ram or flex")
    if test == "^TestFlexColdResourceProfile$" and target != "flex":
        raise SystemExit("the tiered storage profile requires a Flex target")
    output.mkdir(parents=True, exist_ok=True)
    source = (root / "flex_benchmark_profile_test.go").read_text()
    # Use the same numeric process-counter definition as the Go endpoint samples.
    match = re.search(r"const flexBenchProc = `(.+?)`", source, re.S)
    if match is None:
        raise SystemExit("Go profile process-counter definition was not found")
    counters = match.group(1).replace(
        "dict(CPUSeconds=cpu", "dict(UnixNano=time.time_ns(),CPUSeconds=cpu"
    )
    monitor = (
        "import threading,sys,time\n"
        "stop=threading.Event()\n"
        "threading.Thread(target=lambda:(sys.stdin.read(),stop.set()),daemon=True).start()\n"
        "started=time.monotonic()\n"
        "while not stop.is_set() and time.monotonic()-started<1800:\n"
        + "\n".join(" " + line for line in counters.splitlines())
        + "\n stop.wait(0.02)\n"
    )
    timeline = output / (target + "-server-timeline.jsonl")
    fd = os.open(timeline, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        sampler = subprocess.Popen(
            ["docker", "exec", "-i", os.environ["REDIS_FLEX_BENCH_CONTAINER"],
             "python3", "-u", "-c", monitor],
            stdin=subprocess.PIPE, stdout=stream, stderr=subprocess.PIPE, text=True,
        )
        try:
            time.sleep(0.2)
            result = subprocess.run(
                ["go", "test", "-count=1", "-p", "1", "-timeout", "30m",
                 "-v", "-run", test, "."],
                cwd=root,
            )
        finally:
            # EOF stops the in-container sampler; killing docker exec alone can
            # leave its child running. The time limit is an additional safeguard.
            sampler.communicate("", timeout=15)
        if sampler.returncode:
            raise SystemExit("server timeline sampler failed")
    if timeline.stat().st_size == 0:
        raise SystemExit("server timeline sampler produced no samples")
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
