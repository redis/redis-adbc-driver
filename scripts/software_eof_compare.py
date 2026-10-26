#!/usr/bin/env python3
"""Compare #498's SQL reproduction through host forwarding and container loopback.

Use an explicitly named, dedicated Compose project. Raw output stays in a new
private directory; only counts and runtime versions appear in summary.json.
This is a diagnostic, not a replacement for the full integration suite.
"""

import argparse
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import time
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[1]
CASES = r"^TestSQLNaN(Joins|Text)$"


def summarize(output, returncode, count, seconds):
    passes = len(re.findall(r"^--- PASS: TestSQLNaN(?:Joins|Text) ", output, re.M))
    failures = len(re.findall(r"^--- FAIL: TestSQLNaN(?:Joins|Text) ", output, re.M))
    return {
        "returncode": returncode,
        "passed": passes,
        "failed": failures,
        "expected_cases": count * 2,
        "complete": passes + failures == count * 2,
        "eof_lines": sum("EOF" in line for line in output.splitlines()),
        "seconds": round(seconds, 3),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True,
                        help="new private directory outside the checkout")
    parser.add_argument("--count", type=int, default=100)
    parser.add_argument("--timeout", default="4m", help="Go timeout for each run")
    args = parser.parse_args()
    if args.count < 1:
        parser.error("--count must be positive")
    if not os.environ.get("COMPOSE_PROJECT_NAME"):
        parser.error("set COMPOSE_PROJECT_NAME to an owned, dedicated fixture")
    if os.environ.get("SOFTWARE_SHARDS", "3") != "3":
        parser.error("this comparison requires SOFTWARE_SHARDS=3")
    if not os.environ.get("REDIS_URI"):
        parser.error("set REDIS_URI to the fixture's published host endpoint")
    uri = urlsplit(os.environ["REDIS_URI"])
    if (uri.scheme != "redis" or uri.hostname not in {"localhost", "127.0.0.1", "::1"}
            or uri.port != int(os.environ.get("SOFTWARE_DB_PORT", "6379"))
            or uri.path not in {"", "/0"} or uri.query or uri.fragment
            or uri.username is not None or uri.password is not None):
        parser.error("REDIS_URI must address the Compose fixture's loopback port and database 0")
    out = args.output_dir.resolve()
    if out == ROOT or ROOT in out.parents:
        parser.error("raw diagnostic output must stay outside the checkout")
    out.mkdir(mode=0o700, parents=False, exist_ok=False)
    # Other test-specific Redis options must not silently differ between paths.
    env = {k: v for k, v in os.environ.items() if not k.startswith("REDIS_")}
    env.update(REDIS_URI=os.environ["REDIS_URI"], SOFTWARE_SHARDS="3", CGO_ENABLED="0")
    env.setdefault("GOTOOLCHAIN", "go1.26.10")

    def run(command, name, run_env=env):
        start = time.monotonic()
        with (out / name).open("w") as log:
            result = subprocess.run(command, cwd=ROOT, env=run_env,
                                    stdout=log, stderr=subprocess.STDOUT)
        return result.returncode, time.monotonic() - start

    def required(command, name, run_env=env):
        code, _ = run(command, name, run_env)
        if code:
            raise RuntimeError(f"{name} failed; inspect private output")

    # Reused databases must pass effective QPF/topology verification too.
    required(["make", "-C", "go", "redis-up"], "preflight.log")
    compose = ["docker", "compose", "-f", str(ROOT / "go/compose.yaml")]
    container = subprocess.check_output(
        compose + ["ps", "-q", "redis-software"], cwd=ROOT, env=env, text=True
    ).strip()
    if not re.fullmatch(r"[0-9a-f]{12,64}", container):
        raise RuntimeError("expected exactly one running Software container")
    go_version = subprocess.check_output(["go", "version"], env=env, text=True).strip()
    linux_env = dict(env, GOOS="linux")
    # Match the server architecture rather than accidentally using emulation.
    arch = subprocess.check_output(
        ["docker", "exec", container, "uname", "-m"], env=env, text=True
    ).strip()
    linux_env["GOARCH"] = {"aarch64": "arm64", "x86_64": "amd64"}[arch]
    required(["go", "test", "-C", "go", "-c", "-o", str(out / "host.test"), "."],
             "build-host.log")
    required(["go", "test", "-C", "go", "-c", "-o", str(out / "linux.test"), "."],
             "build-linux.log", linux_env)
    flags = [f"-test.count={args.count}", "-test.run", CASES,
             f"-test.timeout={args.timeout}", "-test.v"]
    summary = {"go": go_version, "linux_arch": linux_env["GOARCH"], "runs": {}}
    # These tests own identical table names. Never overlap the two runs.
    code, seconds = run([str(out / "host.test")] + flags, "host.log")
    summary["runs"]["host"] = summarize((out / "host.log").read_text(), code, args.count, seconds)
    required(["make", "-C", "go", "redis-up"], "preflight-linux.log")
    # Copy the static binary into the owned fixture: no runtime image, mounted
    # credentials, host port forwarding or new network endpoint is needed.
    binary = f"/tmp/adbc-eof-498-{secrets.token_hex(6)}.test"
    required(["docker", "cp", str(out / "linux.test"), f"{container}:{binary}"], "copy.log")
    try:
        code, seconds = run([
            "docker", "exec", "-e", "REDIS_URI=redis://127.0.0.1:12000/0",
            container, binary,
        ] + flags, "linux.log")
    finally:
        # docker cp creates a root-owned file in the sticky /tmp directory.
        required(["docker", "exec", "-u", "0", container, "rm", "-f", binary], "cleanup.log")
    summary["runs"]["linux_loopback"] = summarize((out / "linux.log").read_text(), code, args.count, seconds)
    (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary, indent=2))
    return int(any(r["returncode"] or not r["complete"] for r in summary["runs"].values()))


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, subprocess.SubprocessError, KeyError, ValueError) as exc:
        # subprocess command strings may contain private identifiers; keep the
        # failure diagnostic in the private directory, not in public evidence.
        print(f"EOF comparison could not finish ({type(exc).__name__}); inspect private output", file=sys.stderr)
        sys.exit(2)
