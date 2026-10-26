"""Checks for the measurement oracle and cross-engine query boundaries."""
import tempfile
import io
import http.client
from pathlib import Path
import unittest
from unittest import mock

import pyarrow as pa

import cross_database as bench


class CrossDatabaseTests(unittest.TestCase):
    def test_expired_http_connection_retries_before_response(self):
        table = pa.table({'id': [1, 2]})
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, table.schema) as writer:
            writer.write_table(table)

        class Response(io.BytesIO):
            status = 200

        conn = mock.Mock()
        conn.getresponse.side_effect = [http.client.RemoteDisconnected(),
                                       Response(sink.getvalue().to_pybytes())]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'result.arrow'
            result = bench.fetch('clickhouse', {'batch_rows': 4096}, table.schema, None, path, conn)
            bench.validate([path], bench.canonical(table))
        self.assertEqual(result['http_reconnects'], 1)
        self.assertEqual(conn.request.call_count, 2)
        conn.close.assert_called_once()

    def test_spawn_payload_preserves_schema_nullability(self):
        schema = pa.schema([pa.field('id', pa.int64(), nullable=False)])
        with mock.patch.object(bench, 'fetch') as fetch:
            bench.spawned_fetch(('redis', {}, schema, None, 'output'))
            self.assertEqual(fetch.call_args.args[2], schema)

    def test_oracle_preserves_values_and_duplicate_multiplicity(self):
        source = pa.table({'id': pa.array([2**60 + 7, 1, 1], type=pa.int64()),
                           'text': ['a\x00b', None, None]})
        expected = bench.canonical(source)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'result.arrow'

            def write(table):
                with pa.OSFile(str(path), 'wb') as sink, pa.ipc.new_stream(sink, table.schema) as writer:
                    writer.write_table(table)

            write(source.take([2, 0, 1]))
            bench.validate([path], expected)
            for invalid in [source.slice(0, 2), source.take([0, 0, 1]),
                            pa.table({'id': [2**60 + 8, 1, 1], 'text': ['a\x00b', None, None]})]:
                write(invalid)
                with self.assertRaisesRegex(AssertionError, 'multiplicity'):
                    bench.validate([path], expected)

    def test_sql_selects_only_fixture_columns_and_disjoint_bounds(self):
        cfg = {'batch_rows': 4096}
        for engine, ordinal in [('redis', '__rowid'), ('redis_candidate', '__rowid'),
                                ('postgres', '__transfer_rowid'), ('clickhouse', '__transfer_rowid')]:
            sql = bench.query(engine, ['id', 'odd"name'], (25001, 50000), cfg)
            self.assertIn('SELECT "id","odd""name" FROM transferbench.hits', sql)
            self.assertIn(f'WHERE {ordinal} >= 25001 AND {ordinal} <= 50000', sql)
            self.assertNotIn('ORDER BY', sql)
            if engine == 'clickhouse':
                self.assertIn("output_format_arrow_compression_method='none'", sql)

    def test_unsupported_fixture_fails_before_modifying_databases(self):
        with mock.patch.object(bench, 'connection') as connect:
            with self.assertRaises(KeyError):
                bench.load_references(pa.table({'unsupported': [True]}), {})
            connect.assert_not_called()


if __name__ == '__main__':
    unittest.main()
