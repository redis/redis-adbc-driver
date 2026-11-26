"""Flex index compaction gate: synthetic FT.INFO replies only, no Redis or SF1 data.

The reply shape is the RESP3 FT.INFO map that Redis Software 8.8.0 Flex
(Search 8.8.8) returned to redis-py on the 2026-11-26 fixture.
"""

import argparse
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

import flex_compaction
import isolation
import run


INDEX = "adbc_snapshot17_{meta}_" + "0123456789abcdef" * 2 + "_" + "fedcba9876543210" * 2


def ft_info(fields, docs, cycles, keys, name=INDEX, internal=("__rowid", "__adbc_snapshot_deleted")):
    """An FT.INFO map: fields maps identifier -> type; keys maps identifier -> disk_num_keys."""
    identifiers = list(internal) + list(fields)
    types_ = {**{f: "NUMERIC" for f in internal}, **fields}
    return {
        "index_name": name,
        "index_definition": {"key_type": "HASH", "prefixes": ["x~ssb{meta}:" + "0" * 32 + ":"]},
        "attributes": [{"identifier": f, "attribute": f, "type": types_[f], "flags": []} for f in identifiers],
        "num_docs": docs, "max_doc_id": docs,
        "gc_stats": {"bytes_collected": 0.0, "total_ms_run": 0.0, "total_cycles": float(cycles),
                     "average_cycle_time_ms": 0.0, "last_run_time_ms": 0.0},
        "field statistics": [{"identifier": f, "attribute": f,
                              "Index Errors": {"indexing failures": 0},
                              "disk_total_bytes": 0, **({"disk_num_keys": keys[f]} if f in keys else {})}
                             for f in identifiers],
    }


LINEORDER = {"lo_orderkey": "NUMERIC", "lo_partkey": "NUMERIC", "lo_orderpriority": "TAG"}
COLUMNS = {"lineorder": ["lo_orderkey", "lo_partkey", "lo_orderpriority", "lo_comment"],
           "supplier": ["s_suppkey", "s_city"]}


def lineorder(docs=6_001_171, cycles=0, keys=None):
    keys = keys if keys is not None else {f: docs for f in ("__rowid", "__adbc_snapshot_deleted", *LINEORDER)}
    return ft_info(LINEORDER, docs, cycles, keys)


def supplier(docs=2_000, cycles=1, keys=None):
    keys = keys if keys is not None else {"__rowid": 25, "__adbc_snapshot_deleted": 0, "s_suppkey": 25, "s_city": 30}
    return ft_info({"s_suppkey": "NUMERIC", "s_city": "TAG"}, docs, cycles, keys)


COMPACTED_KEYS = {"__rowid": 243_804, "__adbc_snapshot_deleted": 0, "lo_orderkey": 250_000,
                  "lo_partkey": 273_632, "lo_orderpriority": 63_845}


def flat(value):
    """The RESP2 form of a map: a flat key/value list, recursively."""
    if isinstance(value, dict):
        return [item for k, v in value.items() for item in (k, flat(v))]
    if isinstance(value, list):
        return [flat(v) for v in value]
    return value


class IndexStateTests(unittest.TestCase):
    def test_uncompacted_index_is_refused(self):
        state = flex_compaction.index_state(lineorder())
        self.assertEqual(state["state"], "uncompacted")
        self.assertEqual(state["num_docs"], 6_001_171)
        self.assertEqual(state["gc_total_cycles"], 0)
        self.assertEqual(state["numeric_disk_num_keys"]["lo_partkey"], 6_001_171)
        self.assertIn("no completed disk GC cycle", state["problems"])
        self.assertTrue(any("lo_partkey" in p and "num_docs" in p for p in state["problems"]))

    def test_gc_cycle_alone_is_not_enough(self):
        # A cycle that left a NUMERIC field with one entry per document did not compact it.
        keys = {**COMPACTED_KEYS, "lo_partkey": 6_001_171}
        state = flex_compaction.index_state(lineorder(cycles=1, keys=keys))
        self.assertEqual(state["state"], "uncompacted")
        self.assertEqual(state["problems"], ["disk_num_keys not below num_docs for NUMERIC lo_partkey"])

    def test_stale_entries_above_num_docs_are_uncompacted(self):
        # Updated documents keep their old disk entries until GC: keys exceed num_docs.
        keys = {f: 12_000_000 for f in COMPACTED_KEYS}
        state = flex_compaction.index_state(lineorder(cycles=0, keys=keys))
        self.assertEqual(state["state"], "uncompacted")

    def test_few_keys_without_a_completed_cycle_are_refused(self):
        # GC counters live in shard memory: after a restart the layout can be
        # compacted with total_cycles back at 0. The gate does not guess.
        state = flex_compaction.index_state(lineorder(cycles=0, keys=COMPACTED_KEYS))
        self.assertEqual(state["state"], "uncompacted")
        self.assertEqual(state["problems"], ["no completed disk GC cycle"])

    def test_missing_disk_metrics_cannot_pass(self):
        # A RAM (non-Flex) reply has no disk_num_keys: the state cannot be verified.
        state = flex_compaction.index_state(lineorder(cycles=3, keys={}))
        self.assertEqual(state["state"], "uncompacted")
        self.assertTrue(any("no disk_num_keys" in p for p in state["problems"]))

    def test_numeric_field_without_disk_entries_does_not_block(self):
        # The live SSB customer index after its first cycle (2026-11-26): no
        # row has the deletion marker, so FT.INFO reports no disk_num_keys for
        # it. With no entries there is nothing for a term to scan. TAG fields
        # are outside the criterion (c_name has one term per document).
        customer = ft_info({"c_custkey": "NUMERIC", "c_name": "TAG", "c_city": "TAG"}, 30_000, 1,
                           {"__rowid": 1_514, "c_custkey": 1_514, "c_name": 30_000, "c_city": 500})
        state = flex_compaction.index_state(customer)
        self.assertEqual(state["state"], "compacted", state["problems"])
        self.assertIsNone(state["numeric_disk_num_keys"]["__adbc_snapshot_deleted"])
        # Before the cycle the fields with entries still block.
        before = ft_info({"c_custkey": "NUMERIC"}, 30_000, 0, {"__rowid": 30_000, "c_custkey": 30_000})
        self.assertEqual(flex_compaction.index_state(before)["state"], "uncompacted")

    def test_compacted_index_passes_and_records_values(self):
        state = flex_compaction.index_state(lineorder(cycles=1, keys=COMPACTED_KEYS))
        self.assertEqual(state["state"], "compacted")
        self.assertEqual(state["problems"], [])
        self.assertEqual(state["gc_total_cycles"], 1)
        # NUMERIC fields only; TAG fields are not part of the criterion.
        self.assertEqual(set(state["numeric_disk_num_keys"]),
                         {"__rowid", "__adbc_snapshot_deleted", "lo_orderkey", "lo_partkey"})
        self.assertEqual(state["numeric_disk_num_keys"]["lo_partkey"], 273_632)

    def test_index_below_gc_threshold_does_not_block(self):
        # The disk GC needs search-fork-gc-clean-threshold changes; a 25-row table may never get one.
        small = ft_info({"n_nationkey": "NUMERIC"}, 25, 0, {"__rowid": 25, "__adbc_snapshot_deleted": 0, "n_nationkey": 25})
        state = flex_compaction.index_state(small, gc_threshold=100)
        self.assertEqual(state["state"], "below_gc_threshold")
        self.assertEqual(state["problems"], [])
        self.assertEqual(flex_compaction.index_state(small, gc_threshold=10)["state"], "uncompacted")

    def test_resp2_flat_reply_parses_like_resp3(self):
        for info in (lineorder(), lineorder(cycles=1, keys=COMPACTED_KEYS)):
            self.assertEqual(flex_compaction.index_state(flat(info)), flex_compaction.index_state(info))


class EvaluateTests(unittest.TestCase):
    def test_every_loaded_table_needs_a_compacted_index(self):
        state = flex_compaction.evaluate([lineorder(cycles=1, keys=COMPACTED_KEYS), supplier()], COLUMNS)
        self.assertTrue(state["ready"], state["problems"])
        self.assertEqual(state["tables"]["lineorder"]["indexes"][0]["state"], "compacted")
        self.assertEqual(state["tables"]["supplier"]["indexes"][0]["numeric_disk_num_keys"]["s_suppkey"], 25)

        missing = flex_compaction.evaluate([lineorder(cycles=1, keys=COMPACTED_KEYS)], COLUMNS)
        self.assertFalse(missing["ready"])
        self.assertEqual(missing["problems"], ["supplier: no Search index found"])

        slow = flex_compaction.evaluate([lineorder(), supplier()], COLUMNS)
        self.assertFalse(slow["ready"])
        self.assertFalse(slow["tables"]["lineorder"]["ready"])
        self.assertTrue(slow["tables"]["supplier"]["ready"])

    def test_all_of_a_tables_indexes_must_be_compacted(self):
        # A table can have several segments; one uncompacted index still blocks it.
        state = flex_compaction.evaluate([lineorder(cycles=1, keys=COMPACTED_KEYS), lineorder(), supplier()], COLUMNS)
        self.assertFalse(state["ready"])
        self.assertEqual([i["state"] for i in state["tables"]["lineorder"]["indexes"]], ["compacted", "uncompacted"])

    def test_foreign_and_ambiguous_indexes(self):
        other = ft_info({"l_partkey": "NUMERIC"}, 6_001_215, 0, {"__rowid": 6_001_215, "l_partkey": 6_001_215})
        state = flex_compaction.evaluate([lineorder(cycles=1, keys=COMPACTED_KEYS), supplier(), other], COLUMNS)
        self.assertTrue(state["ready"], state["problems"])
        self.assertEqual(state["unmatched_indexes"], 1)

        shared = {"a": ["k"], "b": ["k"]}
        both = ft_info({"k": "NUMERIC"}, 1000, 1, {"__rowid": 10, "__adbc_snapshot_deleted": 0, "k": 10})
        ambiguous = flex_compaction.evaluate([both], shared)
        self.assertFalse(ambiguous["ready"])
        self.assertTrue(any("ambiguous" in p for p in ambiguous["problems"]))

    def test_provenance_excludes_index_names_and_prefixes(self):
        state = flex_compaction.evaluate([lineorder(), supplier()], COLUMNS)
        text = json.dumps(state)
        self.assertNotIn("0123456789abcdef", text)
        self.assertNotIn("{meta}", text)


class WaitTests(unittest.TestCase):
    def clock(self):
        now = [0.0]
        return now, (lambda: now[0]), (lambda seconds: now.__setitem__(0, now[0] + seconds))

    def test_waits_until_every_index_is_compacted(self):
        now, clock, sleep = self.clock()
        replies = iter([[lineorder(), supplier()], [lineorder(), supplier()],
                        [lineorder(cycles=1, keys=COMPACTED_KEYS), supplier()]])
        seen = []
        state = flex_compaction.wait_until_compacted(lambda: next(replies), COLUMNS, wait_seconds=900,
                                                     poll_seconds=15, clock=clock, sleep=sleep,
                                                     progress=seen.append)
        self.assertTrue(state["ready"])
        self.assertEqual(state["polls"], 3)
        self.assertEqual(state["waited_seconds"], 30.0)
        self.assertEqual(state["wait_limit_seconds"], 900)
        self.assertEqual(len(seen), 2)
        self.assertFalse(seen[0]["ready"])

    def test_refuses_after_the_bound(self):
        now, clock, sleep = self.clock()
        polls = []
        def fetch():
            polls.append(now[0])
            return [lineorder(), supplier()]
        with self.assertRaises(flex_compaction.CompactionGateError) as caught:
            flex_compaction.wait_until_compacted(fetch, COLUMNS, wait_seconds=100, poll_seconds=30,
                                                 clock=clock, sleep=sleep)
        # The last poll is at the bound, not after it.
        self.assertEqual(polls, [0, 30, 60, 90, 100])
        self.assertIn("refusing to time", str(caught.exception))
        self.assertIn("lineorder", str(caught.exception))
        self.assertFalse(caught.exception.state["ready"])
        self.assertEqual(caught.exception.state["waited_seconds"], 100.0)

    def test_zero_wait_checks_once(self):
        now, clock, sleep = self.clock()
        calls = []
        def fetch():
            calls.append(1)
            return [lineorder(), supplier()]
        with self.assertRaises(flex_compaction.CompactionGateError):
            flex_compaction.wait_until_compacted(fetch, COLUMNS, wait_seconds=0, clock=clock, sleep=sleep)
        self.assertEqual(len(calls), 1)


class ProfileTests(unittest.TestCase):
    def test_flex_from_driver_option_or_server_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertFalse(flex_compaction.flex_profile(private))
            (private / "connection.json").write_text(json.dumps({"db_kwargs": {"adbc.redis.flex": "true"}}))
            self.assertTrue(flex_compaction.flex_profile(private))
            (private / "connection.json").write_text(json.dumps({"db_kwargs": {"adbc.redis.flex": "false"}}))
            self.assertFalse(flex_compaction.flex_profile(private))
            (private / "server.json").write_text(json.dumps({"search_on_bigstore": True}))
            self.assertTrue(flex_compaction.flex_profile(private))

    def test_gc_threshold_from_verified_server_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertEqual(flex_compaction.gc_threshold(private), 100)
            (private / "server.json").write_text(json.dumps({"search": {"search-fork-gc-clean-threshold": 250}}))
            self.assertEqual(flex_compaction.gc_threshold(private), 250)


DATE_COLUMNS = {"date_": ["d_datekey", "d_date", "d_year"], "lineorder": COLUMNS["lineorder"]}


def date_index(cycles=0, keys=None):
    # The live SSB date_ index, which had no GC cycle 26 min after FT.CREATE.
    keys = keys if keys is not None else {"__rowid": 2_556, "d_datekey": 2_556, "d_year": 2_556, "d_date": 2_556}
    return ft_info({"d_datekey": "NUMERIC", "d_date": "TAG", "d_year": "NUMERIC"}, 2_556, cycles, keys,
                   name="adbc_snapshot17_{meta}_" + "d" * 32 + "_" + "e" * 32)


# The RESP3 FT.PROFILE reply for one point term on the live date_ index.
DATE_PROFILE = {
    "Results": {"attributes": [], "format": "STRING", "results": [], "total_results": 1, "warning": []},
    "Profile": {"Shards": [{"Shard ID": "1", "Total profile time": 2.657083, "Parsing time": 2.191333,
                            "Iterators profile": {"Type": "DISK_NUMERIC", "Time": 0.363458,
                                                  "Number of reading operations": 1,
                                                  "Estimated number of matches": 2556},
                            "Result processors profile": []}], "Coordinator": {}}}


class FakeClient:
    """FT._LIST/FT.INFO/FT.SEARCH/FT.PROFILE over synthetic replies; records commands."""

    def __init__(self, infos):
        self.infos, self.commands = infos, []

    def execute_command(self, *args):
        self.commands.append(args)
        if args[0] == "FT._LIST":
            return [info["index_name"] for info in self.infos]
        info = next(i for i in self.infos if i["index_name"] == args[1])
        if args[0] == "FT.INFO":
            return info
        if args[0] == "FT.SEARCH":
            return {"results": [{"id": "row-key", "extra_attributes": {args[-3]: "19920101"}}], "total_results": 1}
        if args[0] == "FT.PROFILE":
            return DATE_PROFILE
        raise AssertionError(args)

    def close(self):
        pass


def fake_redis(client):
    def fetcher(config):
        def fetch():
            return [client.execute_command("FT.INFO", n) for n in client.execute_command("FT._LIST")]
        return client, fetch
    return fetcher


class ExemptionTests(unittest.TestCase):
    """Opt-in exemption for small indexes that never get a first GC cycle (2026-11-26)."""

    def private(self, tmp, limit=10_000, created=None):
        private = Path(tmp)
        profile = {"uri": "rediss://test:private-secret@localhost:12000/0", "db_kwargs": {"adbc.redis.flex": "true"}}
        if limit is not None:
            profile["flex_compaction_exempt_max_docs"] = limit
        (private / "connection.json").write_text(json.dumps(profile))
        if created is not None:
            (private / "flex-index-created.json").write_text(json.dumps(created))
        return private

    def gate(self, private, infos, now="2026-11-26T04:29:40Z", wait_seconds=0):
        client = FakeClient(infos)
        clock = flex_compaction.datetime.datetime.fromisoformat(now.replace("Z", "+00:00"))
        with patch.object(flex_compaction, "redis_fetch", fake_redis(client)), \
                patch.object(flex_compaction, "utc_now", lambda: clock):
            return flex_compaction.gate(private, DATE_COLUMNS, wait_seconds=wait_seconds), client

    def test_exempt_limit_is_opt_in_and_bounded(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = self.private(tmp, limit=None)
            self.assertEqual(flex_compaction.exempt_max_docs(private), 0)
            for limit in (1, 2_556, 10_000):
                private = self.private(tmp, limit=limit)
                self.assertEqual(flex_compaction.exempt_max_docs(private), limit)
            for invalid in (0, -1, 10_001, True, 5000.0, "5000"):
                private = self.private(tmp, limit=invalid)
                with self.subTest(limit=invalid), self.assertRaisesRegex(ValueError, "1 to 10000"):
                    flex_compaction.exempt_max_docs(private)
            (private / "connection.json").write_text('{"flex_compaction_exempt_max_docs": null}')
            with self.assertRaisesRegex(ValueError, "1 to 10000"):
                flex_compaction.exempt_max_docs(private)

    def test_only_small_uncompacted_indexes_are_exempt(self):
        exempt = flex_compaction.index_state(date_index(), exempt_max_docs=10_000)
        self.assertEqual(exempt["state"], "exempt_uncompacted")
        self.assertEqual(exempt["problems"], [])
        self.assertIn("no completed disk GC cycle", exempt["uncompacted_reasons"])
        self.assertEqual(exempt["max_numeric_disk_num_keys"], 2_556)
        # Above the limit, or with the default, an uncompacted index still blocks.
        self.assertEqual(flex_compaction.index_state(date_index(), exempt_max_docs=2_555)["state"], "uncompacted")
        self.assertEqual(flex_compaction.index_state(date_index())["state"], "uncompacted")
        self.assertEqual(flex_compaction.index_state(lineorder(), exempt_max_docs=10_000)["state"], "uncompacted")
        # A small index that compacted needs no exemption.
        compacted = date_index(cycles=1, keys={"__rowid": 40, "d_datekey": 40, "d_year": 7})
        self.assertEqual(flex_compaction.index_state(compacted, exempt_max_docs=10_000)["state"], "compacted")

    def test_exemption_records_age_and_point_term_profile(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = self.private(tmp, created={"date_": "2026-11-26T04:03:16Z"})
            state, client = self.gate(private, [date_index(), lineorder(cycles=1, keys=COMPACTED_KEYS)])
            self.assertTrue(state["ready"], state["problems"])
            index = state["tables"]["date_"]["indexes"][0]
            self.assertEqual(index["state"], "exempt_uncompacted")
            self.assertEqual((index["num_docs"], index["gc_total_cycles"], index["max_numeric_disk_num_keys"]), (2_556, 0, 2_556))
            self.assertEqual(index["ft_create_utc"], "2026-11-26T04:03:16Z")
            self.assertEqual(index["minutes_since_ft_create"], 26.4)
            self.assertEqual(index["point_term_profile"], [{"field": "d_datekey", "total_ms": 2.657083,
                                                            "iterator": "DISK_NUMERIC", "iterator_ms": 0.363458,
                                                            "estimated_matches": 2556}])
            self.assertEqual(state["exemption"]["max_docs"], 10_000)
            self.assertEqual(state["exemption"]["applied_to"], ["date_"])
            self.assertEqual(state["exemption"]["not_needed"], [])
            # The point term is on the table's first NUMERIC column, read-only.
            profile = next(c for c in client.commands if c[0] == "FT.PROFILE")
            self.assertEqual(profile[2:5], ("SEARCH", "QUERY", "@d_datekey:[19920101 19920101]"))
            self.assertEqual({c[0] for c in client.commands}, {"FT._LIST", "FT.INFO", "FT.SEARCH", "FT.PROFILE"})
            text = json.dumps(state)
            self.assertNotIn("dddddddd", text)
            self.assertNotIn("private-secret", text)

    def test_compacted_small_index_drops_the_exemption(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = self.private(tmp, created={"date_": "2026-11-26T04:03:16Z"})
            compacted = date_index(cycles=1, keys={"__rowid": 40, "d_datekey": 40, "d_year": 7})
            state, client = self.gate(private, [compacted, lineorder(cycles=1, keys=COMPACTED_KEYS)])
            self.assertTrue(state["ready"])
            self.assertEqual(state["tables"]["date_"]["indexes"][0]["state"], "compacted")
            self.assertNotIn("point_term_profile", state["tables"]["date_"]["indexes"][0])
            self.assertEqual(state["exemption"]["applied_to"], [])
            self.assertEqual(state["exemption"]["not_needed"], ["date_"])
            self.assertFalse(any(c[0] == "FT.PROFILE" for c in client.commands))

    def test_exemption_never_passes_large_or_undated_indexes(self):
        with tempfile.TemporaryDirectory() as tmp:
            # Without the creation time the exemption cannot be recorded, so it is refused.
            private = self.private(tmp, created={})
            with self.assertRaises(flex_compaction.CompactionGateError) as caught:
                self.gate(private, [date_index(), lineorder(cycles=1, keys=COMPACTED_KEYS)])
            self.assertIn("date_: exemption needs its FT.CREATE time", str(caught.exception))
            # lineorder above the limit still blocks while uncompacted.
            private = self.private(tmp, created={"date_": "2026-11-26T04:03:16Z"})
            with self.assertRaises(flex_compaction.CompactionGateError) as caught:
                self.gate(private, [date_index(), lineorder()])
            self.assertIn("lineorder: no completed disk GC cycle", str(caught.exception))
            # The default profile is strict.
            private = self.private(tmp, limit=None, created={"date_": "2026-11-26T04:03:16Z"})
            with self.assertRaises(flex_compaction.CompactionGateError) as caught:
                self.gate(private, [date_index(), lineorder(cycles=1, keys=COMPACTED_KEYS)])
            self.assertIn("date_: no completed disk GC cycle", str(caught.exception))

    def test_query_records_carry_the_exemption_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = self.private(tmp, created={"date_": "2026-11-26T04:03:16Z"})
            folder = private / "results" / "ssb"
            (folder / "queries").mkdir(parents=True)
            (folder / "queries" / "1.1.sql").write_text("SELECT 1")
            (folder / "metadata.json").write_text(json.dumps({"flex_index_compaction": {"ready": True, "columns": DATE_COLUMNS}}))
            client = FakeClient([date_index(), lineorder(cycles=1, keys=COMPACTED_KEYS)])
            recorded = []
            argv = ["run.py", "ssb", "--private", str(private), "--output", str(private / "results"),
                    "--reuse-loaded", "--query", "1.1"]
            with patch.object(sys, "argv", argv), patch.object(isolation, "run_query", lambda a: recorded.append(a)), \
                    patch.object(flex_compaction, "redis_fetch", fake_redis(client)), redirect_stdout(io.StringIO()):
                run.main()
            index = recorded[0].flex_compaction["tables"]["date_"]["indexes"][0]
            self.assertEqual(index["state"], "exempt_uncompacted")
            self.assertEqual(index["point_term_profile"][0]["iterator"], "DISK_NUMERIC")
            self.assertIn("minutes_since_ft_create", index)


class FakeDuckDB:
    """The setup phase's source connection: two tables and their columns."""

    def __init__(self):
        self.sql = None

    def execute(self, sql):
        self.sql = sql
        return self

    def fetchall(self):
        if self.sql == "SHOW TABLES":
            return [("lineorder",), ("supplier",)]
        if self.sql.startswith("DESCRIBE"):
            table = self.sql.split('"')[1]
            return [(c, "INTEGER", "YES", None, None, None) for c in COLUMNS[table]]
        if "COUNT" in self.sql:
            return [(10,)]
        raise AssertionError(self.sql)

    def close(self):
        pass


class FakeRedis:
    def cursor(self):
        return self

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass

    def execute(self, sql):
        self.sql = sql

    def fetchone(self):
        return (10,)

    def close(self):
        pass


class HarnessTests(unittest.TestCase):
    def profile(self, private, flex=True):
        (private / "connection.json").write_text(json.dumps({
            "uri": "rediss://test:private-secret@localhost:12000/0",
            "db_kwargs": {"adbc.redis.flex": "true" if flex else "false"}}))
        (private / "server.json").write_text(json.dumps({"search_on_bigstore": flex}))
        return argparse.Namespace(suite="ssb", private=private, output=private / "results", prepare_only=False,
                                  load_only=True, reuse_loaded=True, query=None, duckdb_only=False,
                                  include_duckdb_adbc=False)

    def setup(self, args, fetch, gated=True):
        modules = {"adbc_driver_redis": types.SimpleNamespace(dbapi=types.SimpleNamespace(connect=lambda *a, **k: FakeRedis()))}
        closed = []
        def fetcher(config):
            self.assertEqual(config["uri"], "rediss://test:private-secret@localhost:12000/0")
            return types.SimpleNamespace(close=lambda: closed.append(True)), fetch
        with patch.dict(sys.modules, modules), patch.object(run, "prepare", return_value=(FakeDuckDB(), [("1.1", "")])), \
                patch.object(run.importlib.metadata, "version", return_value="test"), \
                patch.object(isolation, "cgroup_limit", return_value=None), \
                patch.object(flex_compaction, "redis_fetch", fetcher), \
                patch.object(flex_compaction, "POLL_SECONDS", 0), redirect_stdout(io.StringIO()):
            try:
                run.setup(args)
            finally:
                self.assertEqual(closed, [True] if gated else [])
        return args.output / "ssb"

    def events(self, folder):
        return [json.loads(line) for line in (folder / "events.jsonl").read_text().splitlines()]

    def test_setup_waits_and_records_compaction_in_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.profile(Path(tmp))
            replies = iter([[lineorder(), supplier()], [lineorder(cycles=1, keys=COMPACTED_KEYS), supplier()]])
            folder = self.setup(args, lambda: next(replies))
            metadata = json.loads((folder / "metadata.json").read_text())
            gate = metadata["flex_index_compaction"]
            self.assertTrue(gate["ready"])
            self.assertEqual(gate["polls"], 2)
            self.assertEqual(gate["columns"], COLUMNS)
            self.assertEqual(gate["tables"]["lineorder"]["indexes"][0]["gc_total_cycles"], 1)
            self.assertEqual(gate["tables"]["lineorder"]["indexes"][0]["numeric_disk_num_keys"]["lo_partkey"], 273_632)
            kinds = [e["event"] for e in self.events(folder)]
            self.assertEqual(kinds[-3:], ["flex_compaction_wait", "flex_compaction_waiting", "flex_compaction_ready"])
            self.assertNotIn("private-secret", (folder / "metadata.json").read_text() + (folder / "events.jsonl").read_text())

    def test_setup_refuses_to_finish_loading_on_uncompacted_indexes(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.profile(Path(tmp))
            with patch.object(flex_compaction, "WAIT_SECONDS", 0), \
                    self.assertRaises(flex_compaction.CompactionGateError):
                self.setup(args, lambda: [lineorder(), supplier()])
            folder = args.output / "ssb"
            self.assertNotIn("flex_index_compaction", json.loads((folder / "metadata.json").read_text()))
            error = self.events(folder)[-1]
            self.assertEqual(error["event"], "suite_error")
            self.assertFalse(error["flex_index_compaction"]["ready"])
            self.assertEqual(error["flex_index_compaction"]["tables"]["lineorder"]["indexes"][0]["gc_total_cycles"], 0)

    def test_non_flex_setup_has_no_gate(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.profile(Path(tmp), flex=False)
            folder = self.setup(args, lambda: self.fail("non-Flex setup read FT.INFO"), gated=False)
            self.assertNotIn("flex_index_compaction", json.loads((folder / "metadata.json").read_text()))

    def query_main(self, private, gate_state, fetch):
        args = self.profile(private)
        folder = args.output / "ssb"
        (folder / "queries").mkdir(parents=True)
        (folder / "queries" / "1.1.sql").write_text("SELECT 1")
        if gate_state is not None:
            (folder / "metadata.json").write_text(json.dumps({"flex_index_compaction": gate_state}))
        recorded = []
        argv = ["run.py", "ssb", "--private", str(private), "--output", str(args.output), "--reuse-loaded", "--query", "1.1"]
        fetcher = lambda config: (types.SimpleNamespace(close=lambda: None), fetch)
        with patch.object(sys, "argv", argv), patch.object(isolation, "run_query", lambda a: recorded.append(a)), \
                patch.object(flex_compaction, "redis_fetch", fetcher), redirect_stdout(io.StringIO()):
            run.main()
        return recorded, folder

    def test_query_rechecks_compaction_and_records_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorded, _ = self.query_main(Path(tmp), {"ready": True, "columns": COLUMNS},
                                          lambda: [lineorder(cycles=1, keys=COMPACTED_KEYS), supplier()])
            self.assertEqual(len(recorded), 1)
            self.assertTrue(recorded[0].flex_compaction["ready"])
            self.assertEqual(recorded[0].flex_compaction["tables"]["lineorder"]["indexes"][0]["gc_total_cycles"], 1)

    def test_query_refused_when_an_index_is_uncompacted_or_unverified(self):
        for gate_state in ({"ready": True, "columns": COLUMNS}, None):
            with self.subTest(setup_gate=gate_state is not None), tempfile.TemporaryDirectory() as tmp:
                fetch = lambda: [lineorder(), supplier()]
                with self.assertRaises(SystemExit) as caught:
                    self.query_main(Path(tmp), gate_state, fetch)
                self.assertNotEqual(caught.exception.code, 0)
                folder = Path(tmp) / "results" / "ssb"
                refused = [json.loads(l) for l in (folder / "events.jsonl").read_text().splitlines()][-1]
                self.assertEqual(refused["event"], "flex_compaction_refused")
                self.assertEqual(refused["query"], "1.1")

    def test_run_query_records_the_gate_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "ssb").mkdir()
            gate = {"ready": True, "tables": {"lineorder": {"ready": True, "indexes": []}}}
            args = argparse.Namespace(private=root, output=root, suite="ssb", query="1.1", duckdb_only=False,
                                      include_duckdb_adbc=False, flex_compaction=gate)
            runner = lambda cmd, **kwargs: subprocess.CompletedProcess(cmd, 42)
            with redirect_stdout(io.StringIO()):
                record = isolation.run_query(args, runner=runner)
            self.assertEqual(record["flex_index_compaction"], gate)


if __name__ == "__main__":
    unittest.main()
