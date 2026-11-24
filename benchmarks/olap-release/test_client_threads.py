"""Thread-profile controls; mocked engines only, no SQL or network requests."""
import argparse
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

import isolation


class ClientThreadsTests(unittest.TestCase):
    def test_default_and_explicit_profile_bounds(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertEqual(isolation.client_threads(private), 4)
            profile = private / "connection.json"
            profile.write_text('{}')
            self.assertEqual(isolation.client_threads(private), 4)
            for count in (1, 16, 32):
                profile.write_text(json.dumps({"client_threads": count}))
                self.assertEqual(isolation.client_threads(private), count)

    def test_invalid_profile_refused_before_engine_open(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            connect = Mock()
            for invalid in (True, False, 0, -1, 33, 16.0, "16", None, [], {}):
                (private / "connection.json").write_text(json.dumps({"client_threads": invalid}))
                for engine in ("duckdb", "duckdb_adbc"):
                    with self.subTest(value=invalid, engine=engine), patch.dict(sys.modules, {"duckdb": types.SimpleNamespace(connect=connect)}):
                        args = argparse.Namespace(private=private, suite="tpch", engine=engine)
                        with self.assertRaisesRegex(ValueError, "integer from 1 to 32"):
                            isolation.connect_engine(args)
            connect.assert_not_called()

    def test_both_duckdb_paths_receive_16_threads_without_private_provenance(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            (private / "connection.json").write_text(json.dumps({
                "client_threads": 16, "uri": "rediss://test:synthetic-secret@localhost",
                "db_kwargs": {"adbc.redis.default_schema": "public"}}))
            library = private / "synthetic.so"
            library.write_bytes(b"synthetic library; never loaded")
            for engine in ("duckdb", "duckdb_adbc"):
                with self.subTest(engine=engine):
                    conn = Mock()
                    conn.execute.return_value = conn
                    conn.fetchone.return_value = ("synthetic-extension",)
                    connect = Mock(return_value=conn)
                    modules = {"duckdb": types.SimpleNamespace(connect=connect),
                               "adbc_driver_redis": types.SimpleNamespace(_driver_path=lambda: str(library))}
                    args = argparse.Namespace(private=private, suite="tpch", engine=engine)
                    with patch.dict(sys.modules, modules), patch.dict('os.environ', {}, clear=False):
                        actual, provenance = isolation.connect_engine(args)
                    self.assertIs(actual, conn)
                    self.assertEqual(conn.execute.call_args_list[0].args, ("SET threads=16; SET memory_limit='8GB'",))
                    self.assertEqual(provenance["client_threads"], 16)
                    self.assertNotIn("synthetic-secret", json.dumps(provenance))
                    self.assertNotIn(str(private), json.dumps(provenance))
                    if engine == "duckdb":
                        connect.assert_called_once_with(str(private / "tpch-sf1.duckdb"), read_only=True)
                    else:
                        self.assertNotIn(str(private / "tpch-sf1.duckdb"), str(connect.call_args))
                        self.assertTrue(any("query_pushdown false" in call.args[0] for call in conn.execute.call_args_list))


if __name__ == '__main__':
    unittest.main()
