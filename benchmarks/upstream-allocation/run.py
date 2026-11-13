#!/usr/bin/env python3
"""Recreate isolated, pinned upstream allocation fixtures; never start Redis.

Use --mode check for untimed semantic/lifetime checks. Timed runs require an
exclusive host slot coordinated with other active benchmark tasks.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time


ROOT = Path(__file__).resolve().parents[2]
GOREDIS = "v9.23.0"
ARROW = "v18.8.0"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", type=Path, required=True, help="new private artifact directory")
    parser.add_argument("--mode", choices=["check", "bench"], default="check")
    parser.add_argument("--suite", choices=["all", "redis-owned"], default="all")
    parser.add_argument("--benchtime", default="200ms")
    parser.add_argument("--count", type=int, default=3)
    parser.add_argument("--gomaxprocs", type=int, default=1)
    args = parser.parse_args()
    if args.count < 1 or args.gomaxprocs < 1:
        parser.error("count and gomaxprocs must be positive")
    work = args.work.expanduser().resolve()
    if work == ROOT or ROOT in work.parents:
        parser.error("work must be outside the checkout so raw artifacts stay private")
    work.mkdir(mode=0o700, parents=True, exist_ok=False)
    env = dict(os.environ, GOMAXPROCS=str(args.gomaxprocs), GOTOOLCHAIN="go1.27.3")
    for key in list(env):
        if key.startswith("REDIS_"):
            del env[key]

    def module(name):
        return json.loads(subprocess.check_output(["go", "list", "-m", "-json", name], cwd=ROOT / "go", env=env))

    redis = module("github.com/redis/go-redis/v9")
    arrow = module("github.com/apache/arrow-go/v18")
    assert redis["Version"] == GOREDIS and arrow["Version"] == ARROW
    source = Path(redis["Dir"])
    copied = work / "go-redis"
    shutil.copytree(source, copied)
    # The benchmark is NOT an upstream test-suite run. Remove copied root test
    # files to exclude unrelated Redis fixtures/Ginkgo tests; production parser
    # and command sources remain byte-for-byte equal to the pinned module.
    copied.chmod(0o700)
    for old in copied.glob("*_test.go"):
        old.unlink()
    fixture = ROOT / "benchmarks/upstream-allocation/goredis_decode_test.go"
    shutil.copyfile(fixture, copied / "upstream_decode_test.go")
    owned_fixture = ROOT / "benchmarks/upstream-allocation/goredis_owned_test.go"
    if args.suite == "redis-owned":
        shutil.copyfile(owned_fixture, copied / "upstream_owned_test.go")
    shutil.copyfile(ROOT / "LICENSE", copied / "LICENSE.upstream-benchmark-APACHE2")
    files = {
        "redis_command.go": source / "command.go",
        "redis_reader.go": source / "internal/proto/reader.go",
        "redis_license": source / "LICENSE",
        "fixture_license": ROOT / "LICENSE",
        "redis_fixture": fixture,
        "arrow_interface.go": Path(arrow["Dir"]) / "arrow/cdata/interface.go",
        "arrow_license": Path(arrow["Dir"]) / "LICENSE.txt",
        "arrow_exports.go": Path(arrow["Dir"]) / "arrow/cdata/exports.go",
        "arrow_fixture": ROOT / "go/benchmarks/upstreamalloc/bridge_test.go",
        "arrow_bridge": ROOT / "go/benchmarks/upstreamalloc/bridge.go",
    }
    if args.suite == "redis-owned":
        files["redis_owned_fixture"] = owned_fixture
    metadata = {
        "suite": args.suite,
        "expected_cases": 12 if args.suite == "redis-owned" else 28,
        "mode": args.mode,
        "workspace_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "count": args.count,
        "benchtime": args.benchtime,
        "server": "none; byte-reader parser and C Data/C Stream fixtures only",
        "go_version": subprocess.check_output(["go", "version"], cwd=ROOT / "go", text=True, env=env).strip(),
        "gomaxprocs": args.gomaxprocs,
        "redis": {k: redis.get(k) for k in ["Path", "Version", "Sum"]},
        "arrow": {k: arrow.get(k) for k in ["Path", "Version", "Sum"]},
        "sha256": {key: hashlib.sha256(path.read_bytes()).hexdigest() for key, path in files.items()},
        "go_counters": "B/op and allocs/op exclude C malloc/calloc and shared prebuilt payloads",
        "commands": [],
    }
    (work / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")

    def run(name, command, cwd):
        started = time.monotonic()
        with (work / (name + ".log")).open("w") as output:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT)
        metadata["commands"].append({"name": name, "argv": command, "seconds": time.monotonic() - started, "exit_code": result.returncode})
        (work / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
        if result.returncode:
            raise RuntimeError(f"{name} failed; inspect its private log")
        print(name + " passed", flush=True)

    check = "^TestUpstream(RESP3Fixture|OwnedHMGET)$" if args.suite == "redis-owned" else "^TestUpstreamRESP3Fixture$"
    run("redis-check", ["go", "test", "-count=1", "-race", "-p", "1", "-run", check, "."], copied)
    if args.suite == "all":
        run("arrow-check", ["go", "test", "-count=1", "-race", "-p", "1", "./benchmarks/upstreamalloc"], ROOT / "go")
    if args.mode == "bench":
        flags = ["-count=" + str(args.count), "-benchtime=" + args.benchtime, "-benchmem", "-run=^$"]
        benchmark = "^BenchmarkUpstreamHMGETOwned$" if args.suite == "redis-owned" else "^BenchmarkUpstreamHMGETDecode$"
        run("redis-bench", ["go", "test", *flags, "-bench=" + benchmark, "."], copied)
        if args.suite == "all":
            run("arrow-bench", ["go", "test", *flags, "-bench=^BenchmarkArrowHandoff$", "./benchmarks/upstreamalloc"], ROOT / "go")


if __name__ == "__main__":
    main()
