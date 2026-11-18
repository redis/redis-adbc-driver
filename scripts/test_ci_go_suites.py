"""Protect shard completeness, balancing and real runner failure propagation."""
import argparse
import copy
import io
import itertools
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import ci_go_suites as suites


class ShardTests(unittest.TestCase):
    def test_matrix_preserves_each_architecture_and_every_shard(self):
        rows = [{"arch": "amd64", "runner": "x", "integrations": True},
                {"arch": "arm64", "runner": "y", "integrations": False}]
        matrix = suites.expand_matrix({"include": rows}, 4)["include"]
        self.assertEqual(len(matrix), 8)
        for row in rows:
            self.assertEqual([r["shard"] for r in matrix if r["arch"] == row["arch"]], list(range(4)))
            self.assertTrue(all({k: r[k] for k in row} == row for r in matrix if r["arch"] == row["arch"]))

    def test_inventory_uses_go_packages_and_keeps_examples_and_fuzz_seeds(self):
        events = [dict(Action="output", Package=p, Output=n + "\n")
                  for p, n in (("p", "TestOne"), ("p", "Example"), ("p", "FuzzData"),
                               ("p", "BenchmarkIgnored"), ("q", "TestOne"), ("q", "ok  q"))]
        actual = suites.parse_inventory("\n".join(map(json.dumps, events)))
        self.assertEqual(actual, [("p", "Example"), ("p", "FuzzData"), ("p", "TestOne"), ("q", "TestOne")])
        with self.assertRaises(ValueError):
            suites.parse_inventory('{}\n')

    def test_new_tests_run_once_in_each_applicable_original_suite(self):
        inventory = [("p", f"TestNew{i}") for i in range(30)] + [("p", f"TestSnapshotNew{i}") for i in range(30)]
        for suite, count in (("ordinary", 3), ("snapshot", 4)):
            selected = suites.select_suite(inventory, suite)
            groups, loads = suites.partition(selected, count, {})
            flattened = list(itertools.chain.from_iterable(groups))
            self.assertEqual(sorted(flattened), sorted(selected))
            self.assertEqual(len(flattened), len(set(flattened)))
            self.assertEqual(groups, suites.partition(list(reversed(selected)), count, {})[0])
            self.assertLessEqual(max(loads) - min(loads), 0.25)
        self.assertEqual(len(suites.select_suite(inventory, "ordinary")), 60)
        self.assertEqual(len(suites.select_suite(inventory, "snapshot")), 30)

    def test_long_tests_land_on_distinct_shards_and_invalid_plans_fail(self):
        tests = [("p", f"Test{i}") for i in range(12)]
        weights = {f"Test{i}": 100 - i for i in range(4)}
        groups, _ = suites.partition(tests, 4, weights)
        self.assertTrue(all(sum(name in weights for _, name in group) == 1 for group in groups))
        for inventory, count in (([], 3), (tests, 0), (tests + tests, 3)):
            with self.assertRaises(ValueError):
                suites.partition(inventory, count, {})

    def test_command_keeps_race_uncached_sequential_packages_and_exact_names(self):
        for race in (True, False):
            cmd = suites.go_command("p", ["TestOne", "TestSnapshotTwo"], race)
            self.assertIn("-count=1", cmd)
            self.assertIn("-timeout=30m", cmd)
            self.assertEqual(cmd[cmd.index("-p") + 1], "1")
            self.assertEqual("-race" in cmd, race)
            self.assertEqual(cmd[-3:], ["-run", "^(TestOne|TestSnapshotTwo)$", "p"])
        with self.assertRaises(ValueError):
            suites.go_command("p", [], False)

    def test_execution_reports_catch_silent_zero_test_success_and_preserve_failure(self):
        listing = json.dumps(dict(Action="output", Package="p", Output="TestOne\n"))
        for action, code, expected in (("pass", 0, 0), ("skip", 0, 0), ("fail", 1, 1), (None, 0, ValueError)):
            with self.subTest(action=action), tempfile.TemporaryDirectory() as directory:
                args = argparse.Namespace(suite="ordinary", shard=0, shards=1, report=Path(directory)/"ci-ordinary.json")
                process = unittest.mock.Mock()
                process.stdout = io.StringIO(json.dumps(dict(Action=action, Test="TestOne")) + "\n" if action else "")
                process.wait.return_value = code
                process.poll.return_value = code
                with patch.dict(os.environ, {"REDIS_URI": "rediss://test:secret@localhost:1"}, clear=True), \
                     patch.object(suites.subprocess, "run", return_value=argparse.Namespace(stdout=listing)), \
                     patch.object(suites.subprocess, "Popen", return_value=process), \
                     patch.object(suites, "TIMINGS") as timings:
                    timings.read_text.return_value = '{"seconds":{"ordinary":{}}}'
                    if expected is ValueError:
                        with self.assertRaises(ValueError):
                            suites.run_shard(args)
                    else:
                        self.assertEqual(suites.run_shard(args), expected)
                report = json.loads(args.report.read_text())
                self.assertEqual(report.get("complete", False), code == 0 and action is not None)
                self.assertNotIn("secret", args.report.read_text())

    def test_verifier_refuses_missing_duplicate_inconsistent_or_unexecuted_shards(self):
        reports = []
        for suite, count in (("ordinary", 3), ("snapshot", 4)):
            for i in range(count):
                entry = dict(package="p", test=f"Test{i}")
                reports.append(dict(suite=suite, shards=count, shard=i, complete=True,
                                    plan_hash=suite, inventory_count=count, selected=[entry],
                                    results=[dict(entry, action="pass")]))
        mutations = [lambda r: r.pop(), lambda r: r[0].update(shard=1),
                     lambda r: r[0].update(plan_hash="different"),
                     lambda r: r[0].update(complete=False),
                     lambda r: r[0].update(results=[]),
                     lambda r: r[0].update(inventory_count=99)]
        for mutate in [None, *mutations]:
            with self.subTest(mutate=mutate), tempfile.TemporaryDirectory() as directory:
                data = copy.deepcopy(reports)
                if mutate:
                    mutate(data)
                for i, report in enumerate(data):
                    (Path(directory)/f"ci-{i}.json").write_text(json.dumps(report))
                if mutate:
                    with self.assertRaises(ValueError):
                        suites.verify_reports(Path(directory))
                else:
                    suites.verify_reports(Path(directory))


if __name__ == "__main__":
    unittest.main()
