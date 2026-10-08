"""Print per-query hot timings against selected published ClickBench entries.

Requires a full-scale run. Ratios remain provisional until result correctness
is independently verified. No aggregate ranking is assigned.
"""
import argparse
import json
from pathlib import Path


def hot(trials):
    return min(trials[1:]) if all(v is not None for v in trials[1:]) else None


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("run", type=Path)
    ap.add_argument("baselines", type=Path, nargs="+")
    args = ap.parse_args()
    run = json.loads(args.run.read_text())
    meta = run["metadata"]
    from run import validate_placement
    validate_placement(meta["placement"])
    if meta["loaded_rows"] != 99_997_497 or not meta["official_scale"]:
        ap.error("Pilot results cannot be compared numerically to full-scale ClickBench entries")
    checks_path = args.run.with_name(args.run.stem + "-correctness.json")
    checks = json.loads(checks_path.read_text())["checks"] if checks_path.exists() else []
    valid = {(c["query"], c["trial"]) for c in checks if c["status"] == "valid"}
    baselines = [json.loads(path.read_text()) for path in args.baselines]
    if len(run["result"]) != 43 or any(len(b["result"]) != 43 for b in baselines):
        ap.error("Every entry must include 43 query timing triples")
    print("Provisional: validate query results before drawing performance conclusions.\n")
    print("Seconds; hot = min(run 2, run 3). Ratio = Redis seconds / baseline seconds.")
    print("Different hardware, client placement, versions and service capacities apply.\n")
    labels = [b["system"] + " / " + b["machine"] for b in baselines]
    print("| Query | Redis Cloud | " + " | ".join(labels) + " |")
    print("|---|---:|" + "---:|" * len(labels))
    for number, trials in enumerate(run["result"], 1):
        ours = hot(trials)
        verified = (number, 2) in valid and (number, 3) in valid
        cells = [str(number), "failed" if ours is None else f"{ours:.3f}" + (" (unverified)" if not verified else "")]
        if not verified:
            ours = None
        for baseline in baselines:
            other = hot(baseline["result"][number - 1])
            if other is None:
                cells.append("failed")
            elif ours is None or other == 0:
                cells.append(f"{other:.3f}")
            else:
                cells.append(f"{other:.3f} ({ours / other:.2f}×)")
        print("| " + " | ".join(cells) + " |")


if __name__ == "__main__":
    main()
