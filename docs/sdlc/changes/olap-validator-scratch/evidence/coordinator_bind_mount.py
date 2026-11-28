"""Run the real coordinator with the harness's synthetic engines, profile on a bind mount.

Usage (harness at /harness): python -I coordinator_bind_mount.py MOUNT QUERIES
Each query gets a fresh synthetic private profile under MOUNT; nothing reaches a
database. Prints the scratch filesystem/status combinations and any artifact problem.
"""
import collections, io, json, os, sys, tempfile
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, "/harness")
import isolation, test_isolation

mount, count = Path(sys.argv[1]), int(sys.argv[2])
outcomes = collections.Counter()
problems = []
for i in range(count):
    private = Path(tempfile.mkdtemp(dir=mount))
    args, env = test_isolation.synthetic_profile(private)
    with patch.dict(os.environ, env), redirect_stdout(io.StringIO()), \
            patch("diagnostics.flex_tiers", return_value={"split_available": False}):
        record = isolation.run_query(args)
    key = (record.get("validation_scratch_filesystem"),) + tuple(record[f"{e}_status"] for e in isolation.ENGINES)
    outcomes[key] += 1
    for engine in isolation.ENGINES:
        if f"{engine}_artifact_problem" in record:
            problems.append(record[f"{engine}_artifact_problem"])
    leftovers = list(private.glob("query-*")) + list(Path(tempfile.gettempdir()).glob("olap-validate-*"))
    assert not leftovers, leftovers
print(json.dumps({"queries": count, "outcomes": [list(k) + [v] for k, v in outcomes.items()],
                  "artifact_problems": problems}))
