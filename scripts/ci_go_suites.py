"""Balance complete Go test inventories across isolated CI fixtures.

Run from go/. Timings affect placement only; Go discovery determines coverage.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

TIMINGS = Path(__file__).with_name("ci_go_timings.json")
TEST_NAME = re.compile(r"(?:Test|Example|Fuzz)\w*")


def expand_matrix(coverage, shards):
    if shards < 1 or not coverage.get("include"):
        raise ValueError("a nonempty coverage matrix and positive shard count are required")
    return {"include": [dict(row, shard=i, shards=shards)
                        for row in coverage["include"] for i in range(shards)]}


def parse_inventory(output):
    inventory = set()
    packages = set()
    for line in output.splitlines():
        event = json.loads(line)
        package = event.get("Package")
        if package:
            packages.add(package)
        if event.get("Action") != "output":
            continue
        name = event.get("Output", "").strip()
        if TEST_NAME.fullmatch(name):
            if not package:
                raise ValueError("Go listed a test without a package")
            inventory.add((package, name))
    if not inventory or not packages:
        raise ValueError("Go discovery produced no runnable tests")
    return sorted(inventory)


def select_suite(inventory, suite):
    if suite not in ("ordinary", "snapshot"):
        raise ValueError("unknown test suite")
    # Preserve the previous ordinary ./... and snapshot -run Snapshot scopes.
    selected = [test for test in inventory if suite == "ordinary" or "Snapshot" in test[1]]
    if not selected:
        raise ValueError("test suite is empty")
    return selected


def partition(inventory, shards, timings):
    if shards < 1 or len(inventory) < shards or len(set(inventory)) != len(inventory):
        raise ValueError("shards require distinct tests and at least one test each")
    weights = {test: float(timings.get(test[1], 0.25)) for test in inventory}
    if any(not 0 < value < float("inf") for value in weights.values()):
        raise ValueError("invalid scheduling weight")
    weights = {test: max(0.05, value) for test, value in weights.items()}
    groups, loads = [[] for _ in range(shards)], [0.0] * shards
    for test in sorted(inventory, key=lambda test: (-weights[test], test)):
        index = min(range(shards), key=lambda i: (loads[i], i))
        groups[index].append(test)
        loads[index] += weights[test]
    return [sorted(group) for group in groups], loads


def go_command(package, names, race):
    if not names or any(not TEST_NAME.fullmatch(name) for name in names):
        raise ValueError("refusing an empty or invalid test selection")
    command = ["go", "test", "-json", "-v", "-count=1", "-p", "1", "-timeout=30m"]
    if race:
        command.append("-race")
    return command + ["-run", "^(" + "|".join(names) + ")$", package]


def run_shard(args):
    if args.shards < 1 or not 0 <= args.shard < args.shards:
        raise ValueError("shard must be in [0, shards)")
    race = args.suite == "snapshot"
    if not os.environ.get("REDIS_URI"):
        raise ValueError("REDIS_URI must identify the verified Software fixture")
    if race and not os.environ.get("REDIS_SNAPSHOT_URI"):
        raise ValueError("snapshot shards require a dedicated REDIS_SNAPSHOT_URI")
    if not race and os.environ.get("REDIS_SNAPSHOT_URI"):
        raise ValueError("ordinary shards must not run destructive snapshot fixtures")
    # Listing runs TestMain but no tests. Keep it independent of external state.
    listing_env = dict(os.environ)
    for key in ("REDIS_URI", "REDIS_ADMIN_URI", "REDIS_FLEX_URI", "REDIS_SNAPSHOT_URI"):
        listing_env.pop(key, None)
    listing = ["go", "test", "-json", "-p", "1", "-list", "."]
    if race:
        listing.append("-race")
    result = subprocess.run(listing + ["./..."], env=listing_env,
                            text=True, stdout=subprocess.PIPE, check=True)
    inventory = select_suite(parse_inventory(result.stdout), args.suite)
    timings = json.loads(TIMINGS.read_text())["seconds"][args.suite]
    groups, loads = partition(inventory, args.shards, timings)
    selected = groups[args.shard]
    print(f"{args.suite} shard {args.shard + 1}/{args.shards}: "
          f"{len(selected)}/{len(inventory)} tests; estimated {loads[args.shard]:.1f}s", flush=True)
    # Only sanitized names/outcomes/timings enter the artifact, never test output.
    digest = hashlib.sha256(json.dumps(groups, separators=(",", ":")).encode()).hexdigest()
    report = {"plan_hash": digest, "suite": args.suite, "shard": args.shard, "shards": args.shards,
              "inventory_count": len(inventory), "estimated_seconds": loads,
              "selected": [dict(package=p, test=n) for p, n in selected], "results": []}
    expected, completed = set(selected), set()
    args.report.parent.mkdir(parents=True, exist_ok=True)
    try:
        # Packages stay sequential on this shard's database. No t.Parallel changes.
        for package in sorted({p for p, _ in selected}):
            names = [name for p, name in selected if p == package]
            process = subprocess.Popen(go_command(package, names, race), text=True,
                                       stdout=subprocess.PIPE)
            try:
                for line in process.stdout:
                    event = json.loads(line)
                    sys.stdout.write(event.get("Output", ""))
                    sys.stdout.flush()
                    name, action = event.get("Test", ""), event.get("Action")
                    if name and "/" not in name and action in ("pass", "skip", "fail"):
                        completed.add((package, name))
                        report["results"].append(dict(package=package, test=name,
                                                     action=action, seconds=event.get("Elapsed", 0)))
                code = process.wait()
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                process.stdout.close()
            if code:
                return code
        if completed != expected:
            raise ValueError(f"test execution inventory mismatch: "
                             f"missing={sorted(expected - completed)}, unexpected={sorted(completed - expected)}")
        report["complete"] = True
        return 0
    finally:
        args.report.write_text(json.dumps(report, indent=2) + "\n")


def verify_reports(directory):
    reports = [json.loads(path.read_text()) for path in directory.rglob("ci-*.json")]
    if len(reports) != 7:
        raise ValueError("expected three ordinary and four snapshot shard reports")
    for suite, count in (("ordinary", 3), ("snapshot", 4)):
        group = [report for report in reports if report["suite"] == suite]
        if (len(group) != count or {r["shard"] for r in group} != set(range(count))
                or any(r["shards"] != count or not r.get("complete") for r in group)):
            raise ValueError(f"{suite}: incomplete or duplicate shards")
        if len({r["plan_hash"] for r in group}) != 1 or len({r["inventory_count"] for r in group}) != 1:
            raise ValueError(f"{suite}: inconsistent discovery or shard plan")
        selected = [(entry["package"], entry["test"]) for r in group for entry in r["selected"]]
        if len(selected) != len(set(selected)) or len(selected) != group[0]["inventory_count"]:
            raise ValueError(f"{suite}: missing or duplicate test coverage")
        for report in group:
            expected = {(e["package"], e["test"]) for e in report["selected"]}
            completed = {(e["package"], e["test"]) for e in report["results"]}
            if completed != expected or any(e["action"] not in ("pass", "skip") for e in report["results"]):
                raise ValueError(f"{suite}: execution did not complete its selected tests")
        skipped = sum(e["action"] == "skip" for r in group for e in r["results"])
        print(f"{suite}: {len(selected)} unique tests completed, {skipped} skipped (reported explicitly)")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    matrix = commands.add_parser("matrix")
    matrix.add_argument("--coverage", required=True, type=json.loads)
    matrix.add_argument("--shards", required=True, type=int)
    run = commands.add_parser("run")
    run.add_argument("--suite", required=True, choices=("ordinary", "snapshot"))
    run.add_argument("--shard", required=True, type=int)
    run.add_argument("--shards", required=True, type=int)
    run.add_argument("--report", required=True, type=Path)
    verify = commands.add_parser("verify")
    verify.add_argument("--reports", required=True, type=Path)
    args = parser.parse_args()
    if args.command == "matrix":
        print(json.dumps(expand_matrix(args.coverage, args.shards), separators=(",", ":")))
        return 0
    if args.command == "verify":
        verify_reports(args.reports)
        return 0
    return run_shard(args)


if __name__ == "__main__":
    raise SystemExit(main())
