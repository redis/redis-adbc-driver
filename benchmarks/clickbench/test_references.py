import datetime
import json
from pathlib import Path
import tempfile
import unittest

import duckdb
import pyarrow as pa
import pyarrow.parquet as pq

from references import typed_source, generate, checked_reference
from run import convert_batch


class References(unittest.TestCase):
    def test_typed_oracle_matches_arrow_and_temporal_queries(self):
        schema = pa.schema([("EventDate", pa.date32()), ("EventTime", pa.timestamp("us"))])
        batch = pa.record_batch([pa.array([15900, 15901, -1]),
                                 pa.array([1373760061, 1373846579, -1])], names=schema.names)
        with tempfile.TemporaryDirectory() as directory, duckdb.connect() as con:
            path = Path(directory) / "hits.parquet"
            pq.write_table(pa.Table.from_batches([batch]), path)
            typed_source(con, path, schema)
            self.assertEqual(con.execute("SELECT * FROM hits").fetchall(),
                [tuple(row.values()) for row in convert_batch(batch, schema).to_pylist()])
            self.assertEqual(con.execute("SELECT extract(minute FROM EventTime) FROM hits").fetchall(), [(1,), (2,), (59,)])
            self.assertEqual(con.execute("SELECT COUNT(*) FROM hits WHERE EventDate>=DATE '2013-07-14' AND EventTime<TIMESTAMP '2013-07-15'").fetchone(), (1,))
            self.assertEqual(con.execute("SELECT DATE_TRUNC('minute', EventTime) FROM hits ORDER BY EventTime LIMIT 1").fetchone(),
                             (datetime.datetime(1969, 12, 31, 23, 59),))

    def test_all_temporal_clickbench_shapes(self):
        # Official PostgreSQL forms from pinned upstream e9004587.
        queries = [
            'SELECT UserID, extract(minute FROM EventTime) AS m, SearchPhrase, COUNT(*) FROM hits GROUP BY UserID, m, SearchPhrase ORDER BY COUNT(*) DESC LIMIT 10;',
            "SELECT URL, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND DontCountHits = 0 AND IsRefresh = 0 AND URL <> '' GROUP BY URL ORDER BY PageViews DESC LIMIT 10;",
            "SELECT Title, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND DontCountHits = 0 AND IsRefresh = 0 AND Title <> '' GROUP BY Title ORDER BY PageViews DESC LIMIT 10;",
            "SELECT URL, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND IsRefresh = 0 AND IsLink <> 0 AND IsDownload = 0 GROUP BY URL ORDER BY PageViews DESC LIMIT 10 OFFSET 1000;",
            "SELECT TraficSourceID, SearchEngineID, AdvEngineID, CASE WHEN (SearchEngineID = 0 AND AdvEngineID = 0) THEN Referer ELSE '' END AS Src, URL AS Dst, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND IsRefresh = 0 GROUP BY TraficSourceID, SearchEngineID, AdvEngineID, Src, Dst ORDER BY PageViews DESC LIMIT 10 OFFSET 1000;",
            "SELECT URLHash, EventDate, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND IsRefresh = 0 AND TraficSourceID IN (-1, 6) AND RefererHash = 3594120000172545465 GROUP BY URLHash, EventDate ORDER BY PageViews DESC LIMIT 10 OFFSET 100;",
            "SELECT WindowClientWidth, WindowClientHeight, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-01' AND EventDate <= '2013-07-31' AND IsRefresh = 0 AND DontCountHits = 0 AND URLHash = 2868770270353813622 GROUP BY WindowClientWidth, WindowClientHeight ORDER BY PageViews DESC LIMIT 10 OFFSET 10000;",
            "SELECT DATE_TRUNC('minute', EventTime) AS M, COUNT(*) AS PageViews FROM hits WHERE CounterID = 62 AND EventDate >= '2013-07-14' AND EventDate <= '2013-07-15' AND IsRefresh = 0 AND DontCountHits = 0 GROUP BY DATE_TRUNC('minute', EventTime) ORDER BY DATE_TRUNC('minute', EventTime) LIMIT 10 OFFSET 1000;",
        ]
        with tempfile.TemporaryDirectory() as directory, duckdb.connect() as con:
            names = ["UserID", "EventTime", "EventDate", "SearchPhrase", "CounterID", "URL", "Title",
                     "DontCountHits", "IsRefresh", "IsLink", "IsDownload", "TraficSourceID", "SearchEngineID",
                     "AdvEngineID", "Referer", "URLHash", "RefererHash", "WindowClientWidth", "WindowClientHeight"]
            values = [[1], [1373760061], [15900], ["phrase"], [62], ["url"], ["title"], [0], [0], [1], [0],
                      [6], [0], [0], ["referer"], [2868770270353813622], [3594120000172545465], [800], [600]]
            batch = pa.record_batch([pa.array(v) for v in values], names=names)
            schema = pa.schema([(n, pa.timestamp("us") if n == "EventTime" else pa.date32() if n == "EventDate"
                                 else pa.string() if isinstance(values[i][0], str) else pa.int64()) for i, n in enumerate(names)])
            path = Path(directory) / "hits.parquet"
            pq.write_table(pa.Table.from_batches([batch]), path)
            typed_source(con, path, schema)
            for sql in queries:
                rows = con.execute(sql).fetchall()
                self.assertIsInstance(rows, list)

    def test_failed_or_stale_references_not_complete(self):
        with tempfile.TemporaryDirectory() as directory, duckdb.connect() as con:
            output = Path(directory)
            sql = "SELECT 1"
            metadata = {"loaded_rows": 1}
            self.assertFalse(generate(con, [sql] * 42 + ["SELECT missing"], output, metadata))
            with self.assertRaises(ValueError):
                checked_reference(output, 1, sql, metadata)
            self.assertTrue(generate(con, [sql] * 43, output, metadata))
            self.assertEqual(checked_reference(output, 1, sql, metadata), [[1]])
            for number, query, meta in [(2, "SELECT 2", metadata), (1, sql, {"loaded_rows": 2})]:
                with self.assertRaises(ValueError):
                    checked_reference(output, number, query, meta)
            path = output / "q01.json"
            value = json.loads(path.read_text()); value["error"] = "BinderException"
            path.write_text(json.dumps(value))
            with self.assertRaises(ValueError):
                checked_reference(output, 1, sql, metadata)


if __name__ == "__main__":
    unittest.main()
