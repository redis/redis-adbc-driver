"""Per-query diagnostics: the driver sidecar whitelist, the Flex RAM/flash sample
and pass order. Synthetic replies and adapters only; no Redis or benchmark queries."""

import argparse
from contextlib import redirect_stdout
import datetime
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import diagnostics
import flex_compaction
import isolation
import test_isolation


# A driver sidecar shaped like go/execution_diagnostics.go writes it, with the
# literals, names and identifiers a plan can carry.
RAW = {
    "version": 1, "status": "execute_returned", "phase": "select_scan", "sampled_at": "2026-11-26T18:00:00Z",
    "execute_elapsed_seconds": 12.5, "worker_grant": 8,
    "plan": {
        "kind": "select_scan", "hash_scan": False, "index_sort": True,
        "search_result_cap": 10000, "aggregate_result_cap": 0,
        "search_query": "@l_shipdate:[19940101 19941231] @n_name:{BRAZIL}",
        "residual": {"op": "=", "left": {"column": "n_name"}, "right": {"literal": "BRAZIL"}},
        "pushed_limit": [0, 100], "sql_limit": 100, "sql_offset": 7, "late_projection": True, "tie_sort": False,
        "columns": [{"name": "l_shipdate", "field": "l_shipdate", "indexed": True, "tag_values": ["BRAZIL"]},
                    {"name": "n_name", "field": "n_name", "indexed": True, "tag_values": None}],
        "sort": [{"field": "l_shipdate", "descending": True}],
    },
    "processed_rows": 6001215, "matched_rows": 909455, "aggregate_metrics": {},
    "redis_commands": 1234, "redis_round_trip_worker_seconds": 40.25, "socket_read_worker_seconds": 30.5,
    "connection_read_bytes": 123456789, "connection_written_bytes": 98765,
    "io_scope": "connection RESP bytes during execute, including concurrent connection maintenance; excludes TLS framing",
    "redis_server_profile": None,
    "process_allocated_bytes_during_execute": 5_000_000_000, "process_heap_bytes": 300_000_000,
    "index": "idx:secret-index-name", "endpoint": "10.0.0.1:12000",
}

EXPECTED = {
    "version": 1, "status": "execute_returned", "phase": "select_scan",
    "execute_elapsed_seconds": 12.5, "worker_grant": 8, "processed_rows": 6001215, "matched_rows": 909455,
    "redis_commands": 1234, "redis_round_trip_worker_seconds": 40.25, "socket_read_worker_seconds": 30.5,
    "connection_read_bytes": 123456789, "connection_written_bytes": 98765,
    "process_allocated_bytes_during_execute": 5_000_000_000, "process_heap_bytes": 300_000_000,
    "plan": {"kind": "select_scan", "hash_scan": False, "index_sort": True, "search_result_cap": 10000,
             "aggregate_result_cap": 0, "late_projection": True, "tie_sort": False, "index_filter": True,
             "residual": True, "limit_pushed": True, "column_count": 2, "sort_key_count": 1},
}

LEAKS = ("BRAZIL", "l_shipdate", "n_name", "1994", "secret", "10.0.0.1", "idx:")


class SanitizeTests(unittest.TestCase):
    def assertNoLeaks(self, value):
        text = json.dumps(value)
        for leak in LEAKS:
            self.assertNotIn(leak, text)

    def test_metrics_and_plan_structure_are_kept_and_literals_dropped(self):
        clean = diagnostics.sanitize(RAW)
        self.assertEqual(clean, EXPECTED)
        self.assertNoLeaks(clean)

    def test_join_order_inputs_keep_only_the_drivers_sanitized_fields(self):
        plan = {"kind": "lookup_join_order", "order": [1, 0],
                "inputs": [{"input": 0, "source_rows": 6001215, "candidate_rows": 909455, "in_memory": False,
                            "snapshot_overlay": True, "index_filter": True, "residual_filter": False,
                            "local_filter_count": 2, "compiled_scalar_filter": False,
                            "large_published_native": True, "table": "lineitem", "query": "@n_name:{BRAZIL}"},
                           {"input": 1, "source_rows": -1, "candidate_rows": 25, "in_memory": True}],
                "scope": {"snapshot": True, "pin": True, "shared_pin": False, "store_match": True,
                          "maintenance": False, "operation": False, "write": False, "retained": False,
                          "partition_executor": False, "prefix": "adbc:{meta}:secret"}}
        clean = diagnostics.sanitize({"phase": "planning", "plan": plan})
        self.assertEqual(clean["plan"]["order"], [1, 0])
        self.assertEqual(clean["plan"]["inputs"][0], {
            "input": 0, "source_rows": 6001215, "candidate_rows": 909455, "in_memory": False,
            "snapshot_overlay": True, "index_filter": True, "residual_filter": False, "local_filter_count": 2,
            "compiled_scalar_filter": False, "large_published_native": True})
        self.assertEqual(clean["plan"]["inputs"][1], {"input": 1, "source_rows": -1, "candidate_rows": 25,
                                                      "in_memory": True})
        self.assertEqual(set(clean["plan"]["scope"]), set(plan["scope"]) - {"prefix"})
        self.assertNoLeaks(clean)

    def test_partitioned_aggregate_plan_and_metrics(self):
        raw = {"phase": "aggregate_merge",
               "plan": {"kind": "partitioned_exact", "server_source_tuples": True, "client_compact_tuples": False,
                        "compact_probe_max_seconds": 0.004, "partial_groups": True, "count_groups": True,
                        "group_expressions": [{"column": "l_returnflag"}, {"column": "l_linestatus"}],
                        "encoded_group_key_mask": [True, False], "input_columns": ["l_quantity", "l_shipdate"],
                        "residual": None, "search_query": "*", "partition_rows": 65536},
               "aggregate_metrics": {"strategy": "partitioned_exact", "workers": 8, "partition_attempts": 92,
                                     "group_limit_retries": 0, "row_fallbacks": 1, "returned_tuples": 3_000,
                                     "represented_rows": 6001215, "logical_value_bytes": 12_000,
                                     "redis_reply_seconds": 3.5, "merge_elapsed_seconds": float("inf"),
                                     "spill_written_bytes": 0, "columns": ["l_shipdate"]}}
        clean = diagnostics.sanitize(raw)
        self.assertEqual(clean["plan"], {
            "kind": "partitioned_exact", "server_source_tuples": True, "client_compact_tuples": False,
            "compact_probe_max_seconds": 0.004, "partial_groups": True, "count_groups": True,
            "encoded_group_key_mask": [True, False], "partition_rows": 65536, "index_filter": False,
            "residual": False, "input_column_count": 2, "group_expression_count": 2})
        self.assertEqual(clean["aggregate_metrics"], {
            "strategy": "partitioned_exact", "workers": 8, "partition_attempts": 92, "group_limit_retries": 0,
            "row_fallbacks": 1, "returned_tuples": 3_000, "represented_rows": 6001215,
            "logical_value_bytes": 12_000, "redis_reply_seconds": 3.5, "spill_written_bytes": 0})
        self.assertNoLeaks(clean)

    def test_identifiers_types_and_unknown_fields(self):
        raw = {"status": "weird", "phase": "select scan where n_name = 'BRAZIL'", "worker_grant": "8",
               "processed_rows": True, "matched_rows": float("nan"), "redis_commands": 3,
               "plan": {"kind": "eager_join_fallback", "reason": "filtered_dimension_cap", "snowflake": False,
                        "source": "'BRAZIL'", "new_driver_field": 1, "dimension_rows": [10, 25, "x"]}}
        clean = diagnostics.sanitize(raw)
        self.assertEqual(clean, {"redis_commands": 3,
                                 "plan": {"kind": "eager_join_fallback", "reason": "filtered_dimension_cap",
                                          "snowflake": False}})
        self.assertEqual(diagnostics.sanitize({"plan": "not a plan", "aggregate_metrics": []}), {})
        self.assertEqual(diagnostics.sanitize({"plan": {"kind": "Select Scan"}}), {"plan": {}})
        # A plan that is not yet chosen is recorded as such.
        self.assertEqual(diagnostics.sanitize({"phase": "planning", "plan": None}), {"phase": "planning"})


class FakeInfoClient:
    def __init__(self, sections, error=None):
        self.sections, self.error, self.calls, self.closed = sections, error, [], False

    def info(self, section):
        self.calls.append(section)
        if self.error:
            raise self.error
        return self.sections.get(section, {})

    def close(self):
        self.closed = True


PROXY = {"keyspace": {"db0": {"keys": 6151256, "expires": 0, "avg_ttl": 0}},
         "memory": {"used_memory": 4851870684, "used_memory_human": "4.52G", "maxmemory_policy": "noeviction"}}
SHARD = {**PROXY, "Bigredis-Keyspace": {"bigdb0": {"keys": 6151256, "expires": 0, "avg_ttl": 0, "subexpiry": 0,
                                                   "clean": 78, "dirty": 2953523, "disk": 4460583,
                                                   "ram": 2953601, "meta": 163243}}}
NOW = datetime.datetime(2026, 11, 26, 18, 45, 1, 250000, tzinfo=datetime.timezone.utc)


class FlexTierTests(unittest.TestCase):
    def private(self, tmp):
        private = Path(tmp)
        (private / "connection.json").write_text(json.dumps({
            "uri": "rediss://test:private-secret@localhost:12000/0", "db_kwargs": {"adbc.redis.flex": "true"}}))
        return private

    def sample(self, client):
        with tempfile.TemporaryDirectory() as tmp:
            private = self.private(tmp)
            def connect(config):
                self.assertEqual(config["uri"], "rediss://test:private-secret@localhost:12000/0")
                return client
            with patch.object(flex_compaction, "redis_client", connect), patch.object(diagnostics, "utc_now", lambda: NOW):
                return diagnostics.flex_tiers(private)

    def test_proxy_reports_no_split(self):
        # Redis Software 8.8 Flex: the endpoint answers no Bigredis section and
        # keeps only keys/expires/avg_ttl on db0 (live fixture, 2026-11-26).
        client = FakeInfoClient(PROXY)
        self.assertEqual(self.sample(client), {
            "captured_utc": "2026-11-26T18:45:01.250000+00:00", "keys": 6151256,
            "endpoint_used_memory": 4851870684, "ram_objects": None, "flash_objects": None,
            "split_available": False})
        self.assertEqual(client.calls, ["keyspace", "Bigredis-Keyspace", "memory"])
        self.assertTrue(client.closed)

    def test_shard_format_reports_the_split(self):
        state = self.sample(FakeInfoClient(SHARD))
        self.assertEqual((state["ram_objects"], state["flash_objects"], state["split_available"]),
                         (2953601, 4460583, True))

    def test_empty_database_and_partial_or_odd_replies(self):
        self.assertEqual(self.sample(FakeInfoClient({}))["keys"], 0)
        partial = {"Bigredis-Keyspace": {"bigdb0": {"ram": 5, "disk": "x"}}, "memory": {"used_memory": True}}
        state = self.sample(FakeInfoClient(partial))
        self.assertEqual((state["ram_objects"], state["flash_objects"], state["split_available"],
                          state["endpoint_used_memory"]), (5, None, False, None))

    def test_errors_are_recorded_by_type_and_never_raise(self):
        error = ConnectionError("Error connecting to rediss://test:private-secret@localhost:12000/0")
        client = FakeInfoClient(PROXY, error=error)
        state = self.sample(client)
        self.assertEqual(state, {"captured_utc": "2026-11-26T18:45:01.250000+00:00", "error": "ConnectionError"})
        self.assertTrue(client.closed)
        with tempfile.TemporaryDirectory() as tmp:
            def missing(config):
                raise ModuleNotFoundError("No module named 'redis'")
            with patch.object(flex_compaction, "redis_client", missing):
                self.assertEqual(diagnostics.flex_tiers(self.private(tmp))["error"], "ModuleNotFoundError")

    def test_diagnostics_module_imports_no_engine(self):
        harness = Path(__file__).resolve().parent
        code = ("import sys, diagnostics; "
                "print(sorted({'duckdb', 'adbc_driver_redis', 'adbc_driver_manager', 'pyarrow', 'redis'} & set(sys.modules)))")
        loaded = subprocess.run([sys.executable, "-c", code], cwd=harness, capture_output=True, text=True, check=True)
        self.assertEqual(loaded.stdout.strip(), "[]")


# What the reference sampler printed for the measured shard, plus fields a
# caller's sampler must never get into a record.
SAMPLE = {"source": "management_api_bdb_stats", "ram_objects": 7803390, "flash_objects": 8190741,
          "ram_clean_objects": 6938217, "ram_dirty_objects": 865173, "keys": 8661322,
          "used_memory_bytes": 13444720436, "used_flash_bytes": 6374828452.0,
          "interval_start_utc": "2026-11-26T18:43:26Z", "interval_end_utc": "2026-11-26T18:43:27Z"}
CLEAN_SAMPLE = {**SAMPLE, "used_flash_bytes": 6374828452, "interval_start_utc": "2026-11-26T18:43:26+00:00",
                "interval_end_utc": "2026-11-26T18:43:27+00:00"}
UNSAFE = {"password": "private-secret", "url": "https://admin:private-secret@localhost:9443",
          "bdb_name": "secret-db", "note": "private-secret"}


class SamplerTests(unittest.TestCase):
    """The opt-in flex_tier_sampler command (a caller-owned process, never run.py)."""

    def private(self, tmp, script=None, command=None):
        private = Path(tmp)
        profile = {"uri": "rediss://test:private-secret@localhost:12000/0", "db_kwargs": {"adbc.redis.flex": "true"}}
        if script is not None:
            (private / "sampler.py").write_text(script)
            command = [sys.executable, str(private / "sampler.py")]
        if command is not None:
            profile["flex_tier_sampler"] = command
        (private / "connection.json").write_text(json.dumps(profile))
        return private

    def sample(self, private, sections=PROXY, error=None):
        client = FakeInfoClient(sections, error=error)
        with patch.object(flex_compaction, "redis_client", lambda config: client), \
                patch.object(diagnostics, "utc_now", lambda: NOW):
            state = diagnostics.flex_tiers(private)
        self.assertNotIn("private-secret", json.dumps(state))
        self.assertNotIn("secret-db", json.dumps(state))
        return state

    def printing(self, value):
        return f"import json\nprint(json.dumps({value!r}))\n"

    def test_sampler_supplies_the_split_and_is_whitelisted(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = self.sample(self.private(tmp, self.printing({**SAMPLE, **UNSAFE, "ram_objects_estimate": 1})))
            sampler = dict(state["sampler"])
            self.assertGreaterEqual(sampler.pop("seconds"), 0)
            self.assertEqual(sampler, CLEAN_SAMPLE)
            self.assertEqual((state["ram_objects"], state["flash_objects"], state["split_available"],
                              state["split_source"]), (7803390, 8190741, True, "external_sampler"))
            # The endpoint's own values are kept alongside.
            self.assertEqual((state["keys"], state["endpoint_used_memory"]), (6151256, 4851870684))

    def test_an_endpoint_split_wins_and_the_sample_is_kept(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = self.sample(self.private(tmp, self.printing(SAMPLE)), sections=SHARD)
            self.assertEqual((state["ram_objects"], state["flash_objects"], state["split_source"]),
                             (2953601, 4460583, "endpoint_info"))
            self.assertEqual(state["sampler"]["ram_objects"], 7803390)

    def test_a_failed_info_sample_still_runs_the_sampler(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = self.sample(self.private(tmp, self.printing(SAMPLE)), error=ConnectionError("rediss://x"))
            self.assertEqual(state["error"], "ConnectionError")
            self.assertEqual((state["ram_objects"], state["split_available"], state["split_source"]),
                             (7803390, True, "external_sampler"))

    def test_failures_are_recorded_without_any_output(self):
        leak = "import sys\nprint('{\"ram_objects\": 1, \"password\": \"private-secret\"}')\n" \
               "print('private-secret', file=sys.stderr)\n"
        cases = {
            "exited with code 3": leak + "sys.exit(3)\n",
            "output is not one JSON object": "print('ram_objects=1 private-secret')\n",
            "output is larger than 64 KiB": "print('{\"keys\": 1, \"pad\": \"' + 'x' * 70000 + '\"}')\n",
            "timed out after 0.5 s": "import time\ntime.sleep(10)\n",
        }
        for error, script in cases.items():
            with self.subTest(error=error), tempfile.TemporaryDirectory() as tmp, \
                    patch.object(diagnostics, "SAMPLER_TIMEOUT_SECONDS", 0.5):
                state = self.sample(self.private(tmp, script))
                self.assertEqual(set(state["sampler"]), {"error", "seconds"})
                self.assertEqual(state["sampler"]["error"], error)
                self.assertFalse(state["split_available"])
                self.assertNotIn("split_source", state)
        with tempfile.TemporaryDirectory() as tmp:
            state = self.sample(self.private(tmp, command=[str(Path(tmp) / "missing-sampler")]))
            self.assertEqual(state["sampler"]["error"], "FileNotFoundError")

    def test_invalid_values_are_dropped(self):
        odd = {"ram_objects": -1, "flash_objects": "5", "keys": True, "used_memory_bytes": 1.5,
               "interval_end_utc": "yesterday", "interval_start_utc": "2026-11-26T18:43:26",
               "source": "Management API", "ram_clean_objects": float("inf")}
        with tempfile.TemporaryDirectory() as tmp:
            script = f"print({json.dumps(odd).replace('Infinity', '1e999')!r})\n"
            state = self.sample(self.private(tmp, script))
            self.assertEqual(set(state["sampler"]), {"seconds"})
            self.assertFalse(state["split_available"])

    def test_no_sampler_means_no_sampler_field(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertNotIn("sampler", self.sample(self.private(tmp)))

    def test_option_is_validated_before_any_engine(self):
        for command in ("python3 sampler.py", [], ["python3", 2], [""], {"command": ["x"]}):
            with self.subTest(command=command), tempfile.TemporaryDirectory() as tmp:
                private = self.private(tmp, command=command)
                with self.assertRaisesRegex(ValueError, "flex_tier_sampler"):
                    diagnostics.tier_sampler(private)
                (private / "tpch").mkdir()
                args = argparse.Namespace(private=private, output=private, suite="tpch", query="19",
                                          duckdb_only=False, include_duckdb_adbc=False)
                runner = lambda cmd, **kwargs: self.fail("an engine ran with an invalid sampler option")
                with self.assertRaisesRegex(ValueError, "flex_tier_sampler"), redirect_stdout(io.StringIO()):
                    isolation.run_query(args, runner=runner)
        with tempfile.TemporaryDirectory() as tmp:
            self.assertIsNone(diagnostics.tier_sampler(Path(tmp)))
            self.assertEqual(diagnostics.tier_sampler(self.private(tmp, command=["sampler", "--x"])),
                             ["sampler", "--x"])


def fake_runner(events, folder):
    """Engine workers that succeed without running anything; logs phases and checkpoints."""
    def runner(cmd, **kwargs):
        phase, engine = cmd[cmd.index("--phase") + 1], cmd[cmd.index("--engine") + 1]
        query = cmd[cmd.index("--query") + 1]
        events.append((phase, engine))
        if phase == "engine":
            checkpoint = json.loads((folder / f"checkpoint-{query}.json").read_text())
            events.append(("checkpoint", checkpoint.get("pass_order")))
            artifacts = Path(cmd[cmd.index("--artifacts") + 1])
            (artifacts / f"{engine}.json").write_text(json.dumps({
                f"{engine}_status": "executed", f"{engine}_median_seconds": 1.0, "pid": 1}))
        return subprocess.CompletedProcess(cmd, 0)
    return runner


class CoordinatorTests(unittest.TestCase):
    def args(self, root, flex=True, query="19"):
        (root / "connection.json").write_text(json.dumps({
            "uri": "rediss://test:private-secret@localhost:12000/0",
            "db_kwargs": {"adbc.redis.flex": "true" if flex else "false"}}))
        (root / "tpch").mkdir(exist_ok=True)
        return argparse.Namespace(private=root, output=root, suite="tpch", query=query, duckdb_only=False,
                                  include_duckdb_adbc=True)

    def run_query(self, args, events, tiers=None):
        def sample(private):
            events.append(("tiers",))
            return {"captured_utc": "t", "split_available": False, "keys": len(events)}
        with patch.object(diagnostics, "flex_tiers", tiers or sample), redirect_stdout(io.StringIO()):
            return isolation.run_query(args, runner=fake_runner(events, args.output / "tpch"))

    def test_split_is_sampled_just_before_each_redis_backed_engine_on_flex(self):
        with tempfile.TemporaryDirectory() as tmp:
            events = []
            record = self.run_query(self.args(Path(tmp)), events)
            starts = [i for i, e in enumerate(events) if e[0] == "engine"]
            self.assertEqual([events[i][1] for i in starts], ["duckdb", "redis", "duckdb_adbc"])
            self.assertNotEqual(events[starts[0] - 1], ("tiers",))
            for engine, start in zip(["redis", "duckdb_adbc"], starts[1:]):
                self.assertEqual(events[start - 1], ("tiers",))
                self.assertEqual(record[f"{engine}_flex_tiers_before"]["keys"], start)
            self.assertNotIn("duckdb_flex_tiers_before", record)
            for engine in isolation.ENGINES:
                self.assertEqual(record[f"{engine}_status"], "ok")

    def test_ram_profile_is_never_sampled(self):
        with tempfile.TemporaryDirectory() as tmp:
            record = self.run_query(self.args(Path(tmp), flex=False), [],
                                    tiers=lambda private: self.fail("RAM profile sampled the split"))
            self.assertFalse(any(k.endswith("_flex_tiers_before") for k in record))

    def test_pass_order_lists_the_queries_already_recorded(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            engines = ["duckdb", "redis", "duckdb_adbc"]
            events = []
            first = self.run_query(self.args(root, query="19"), events)
            self.assertEqual(first["pass_order"], {"position": 1, "queries_before": [], "engines": engines})
            # The checkpoint carries it from the start, so a supervisor-written failure keeps it.
            self.assertEqual(events[1], ("checkpoint", first["pass_order"]))
            second = self.run_query(self.args(root, query="2"), [])
            self.assertEqual(second["pass_order"], {"position": 2, "queries_before": ["19"], "engines": engines})
            # A coordinator failure that supervise.py appended from its checkpoint.
            with (root / "tpch" / "queries.jsonl").open("a") as out:
                out.write(json.dumps({"suite": "tpch", "query": "3", "engine_isolation": "sequential",
                                      "coordinator_error": "query coordinator exceeded 1200-second process limit"}) + "\n")
            fourth = self.run_query(self.args(root, query="4"), [])
            self.assertEqual(fourth["pass_order"]["queries_before"], ["19", "2", "3"])
            self.assertEqual(fourth["pass_order"]["position"], 4)

    def test_duckdb_only_pass_order_and_no_redis_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.args(Path(tmp))
            args.duckdb_only, args.include_duckdb_adbc = True, False
            record = self.run_query(args, [], tiers=lambda private: self.fail("DuckDB-only run sampled Redis"))
            self.assertEqual(record["pass_order"], {"position": 1, "queries_before": [], "engines": ["duckdb"]})
            self.assertNotIn("duckdb_adbc_execution_diagnostics_unavailable", record)


class WorkerDiagnosticsTests(unittest.TestCase):
    """Real engine/validator child processes with the synthetic driver (test_isolation)."""

    exercise_workers = test_isolation.IsolationTests.exercise_workers

    def options(self, private, record):
        lines = (private / "statement-options.jsonl").read_text().splitlines()
        self.recorded_options = [json.loads(line) for line in lines]

    def test_only_the_warmup_is_instrumented_and_the_record_is_whitelisted(self):
        record = self.exercise_workers(inspect=self.options, extra_env={"FAKE_SIDECAR": json.dumps(RAW)})
        self.assertEqual(record["redis_status"], "ok")
        self.assertEqual(record["redis_warmup_execution_diagnostics"], EXPECTED)
        self.assertEqual(len(self.recorded_options), 1)
        option = self.recorded_options[0]
        self.assertEqual(option["engine"], "redis")
        path = Path(option["options"]["adbc.redis.execution_diagnostics_file"])
        self.assertEqual(path.name, "redis-warmup-diagnostics.json")
        self.assertTrue(path.parent.name.startswith("query-"))
        for leak in LEAKS:
            self.assertNotIn(leak, json.dumps(record))
        self.assertNotIn("redis_execution_diagnostics_error", record)
        self.assertNotIn("duckdb_warmup_execution_diagnostics", record)
        self.assertIn("own statements", record["duckdb_adbc_execution_diagnostics_unavailable"])

    def test_crashed_worker_keeps_the_last_running_sample(self):
        record = self.exercise_workers(failure="redis", extra_env={"FAKE_SIDECAR": json.dumps(RAW)})
        self.assertEqual(record["redis_status"], "worker_failed")
        self.assertEqual(record["redis_warmup_execution_diagnostics"], {**EXPECTED, "status": "running"})

    def test_rejected_option_is_recorded_and_the_query_still_runs(self):
        record = self.exercise_workers(extra_env={"REJECT_DIAGNOSTICS": "redis", "FAKE_SIDECAR": json.dumps(RAW)})
        self.assertEqual(record["redis_status"], "ok")
        self.assertEqual(len(record["redis_seconds"]), 3)
        self.assertIn("NOT_IMPLEMENTED", record["redis_execution_diagnostics_error"])
        self.assertNotIn("private-secret", record["redis_execution_diagnostics_error"])
        self.assertNotIn("redis_warmup_execution_diagnostics", record)

    def test_unreadable_sidecar_is_flagged_not_copied(self):
        def corrupt(artifacts, engine):
            if engine == "redis":
                (artifacts / "redis-warmup-diagnostics.json").write_text('{"plan": {"search_query": "BRAZIL"')
        record = self.exercise_workers(corrupt=corrupt, extra_env={"FAKE_SIDECAR": json.dumps(RAW)})
        self.assertEqual(record["redis_warmup_execution_diagnostics"], {"unreadable": "malformed"})
        self.assertEqual(record["redis_status"], "ok")


if __name__ == "__main__":
    unittest.main()
