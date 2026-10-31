"""Summarize current per-environment JUnit evidence without hiding non-passes."""

import json
from pathlib import Path
import sys
import xml.etree.ElementTree as ET


def main():
    root = Path(sys.argv[1])
    profiles = ["core", "workflows", "models", "orchestrators", "spark", "airflow", "superset", "dbt", "documents", "ray-tls"]
    summary = {}
    failed = False
    for profile in profiles:
        path = root / f"{profile}.xml"
        exit_path = root / f"{profile}.exitcode"
        exit_code = int(exit_path.read_text()) if exit_path.exists() else None
        if not path.exists():
            summary[profile] = {"status": "no_result", "exit_code": exit_code}
            print(f"{profile}: no current test report; inspect {profile}.log")
            failed = True
            continue
        counts = {"passed": 0, "failed": 0, "errors": 0, "skipped": 0, "xfailed": 0}
        for case in ET.parse(path).iter("testcase"):
            skip = case.find("skipped")
            if case.find("failure") is not None:
                result = "failed"
            elif case.find("error") is not None:
                result = "errors"
            elif skip is not None:
                result = "xfailed" if skip.get("type") == "pytest.xfail" else "skipped"
            else:
                result = "passed"
            counts[result] += 1
        summary[profile] = {"status": "completed" if exit_code == 0 else "runner_failed",
                            "exit_code": exit_code, **counts}
        failed |= bool(counts["failed"] or counts["errors"] or exit_code != 0)
        print(profile + ": " + ", ".join(f"{n} {label}" for label, n in counts.items())
              + f"; runner exit {exit_code}")
    (root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
