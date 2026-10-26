"""Check source-data decoding and incomplete timing handling without a server."""
import unittest
from unittest.mock import Mock

import pyarrow as pa
import redis

from compare import hot
from run import convert_batch, deployment_uri, index_status, ingest_batch, source_batches, validate_placement


class Helpers(unittest.TestCase):
    def test_ingest_names_match_unquoted_postgres_ddl(self):
        batch = pa.RecordBatch.from_arrays([pa.array([42], type=pa.int64())],
            schema=pa.schema([pa.field("WatchID", pa.int64(), nullable=False)]))
        actual = ingest_batch(batch)
        self.assertEqual(actual.schema.names, ["watchid"])
        self.assertFalse(actual.schema.field(0).nullable)
        self.assertEqual(actual.to_pylist(), [{"watchid": 42}])
        self.assertEqual(batch.schema.names, ["WatchID"])

    def test_index_status_requires_all_cluster_primaries(self):
        client = Mock(spec=redis.RedisCluster)
        nodes = [object(), object(), object()]
        client.get_primaries.return_value = nodes
        client.execute_command.side_effect = [
            [b"num_docs", b"30", b"indexing", b"0"],
            [b"num_docs", b"20", b"indexing", b"1"],
            {"num_docs": "50", "indexing": "0"},
        ]
        self.assertEqual(index_status(client, "idx:pilot:hits"), ([30, 20, 50], 1))
        self.assertEqual(client.execute_command.call_count, 3)
        for call, node in zip(client.execute_command.call_args_list, nodes):
            self.assertEqual(call.kwargs, {"target_nodes": node})

    def test_self_hosted_is_explicit_and_loopback_only(self):
        local = {"REDIS_URI": "redis://127.0.0.1:6379/0"}
        self.assertEqual(deployment_uri("self-hosted", local), local["REDIS_URI"])
        with self.assertRaises(ValueError):
            deployment_uri("cloud", local)
        for uri in ("redis://remote.example:6379", "http://localhost:6379",
                    "redis://localhost.attacker.example:6379"):
            with self.assertRaises(ValueError):
                deployment_uri("self-hosted", {"REDIS_URI": uri})
        cloud = {"REDIS_CLOUD_URI": "rediss://example.redis-cloud.com:1234"}
        self.assertEqual(deployment_uri("cloud", cloud), cloud["REDIS_CLOUD_URI"])
        with self.assertRaises(ValueError):
            deployment_uri("cloud", {"REDIS_CLOUD_URI": local["REDIS_URI"]})

    def test_dedicated_client_requires_verified_private_seed(self):
        uri = {"REDIS_URI": "redis://172.31.20.3:6379"}
        self.assertEqual(deployment_uri("self-hosted", uri, ["172.31.20.3"]), uri["REDIS_URI"])
        with self.assertRaises(ValueError):
            deployment_uri("self-hosted", uri, ["172.31.20.4"])
        with self.assertRaises(ValueError):
            deployment_uri("self-hosted", {"REDIS_URI": "redis://8.8.8.8:6379"}, ["8.8.8.8"])

    def test_dirty_parquet_date_and_timestamp(self):
        schema = pa.schema([pa.field("EventDate", pa.date32(), nullable=False),
                            pa.field("EventTime", pa.timestamp("us"), nullable=False),
                            pa.field("Title", pa.string())])
        batch = pa.record_batch([pa.array([15900], type=pa.uint16()), pa.array([1373760000]),
                                 pa.array([b"Google"], type=pa.binary())],
                                names=schema.names)
        row = convert_batch(batch, schema).to_pylist()[0]
        self.assertEqual(str(row["EventDate"]), "2013-07-14")
        self.assertEqual(str(row["EventTime"]), "2013-07-14 00:00:00")
        self.assertEqual(row["Title"], "Google")

    def test_overflow_not_silently_truncated(self):
        batch = pa.record_batch([pa.array([32768])], names=["ResolutionWidth"])
        with self.assertRaises(pa.ArrowInvalid):
            convert_batch(batch, pa.schema([("ResolutionWidth", pa.int16())]))

    def test_null_in_required_column(self):
        batch = pa.record_batch([pa.array([None], type=pa.int64())], names=["UserID"])
        with self.assertRaisesRegex(ValueError, "NULL values"):
            convert_batch(batch, pa.schema([pa.field("UserID", pa.int64(), nullable=False)]))

    def test_source_prefix_and_resume_across_batches(self):
        schema = pa.schema([("id", pa.int64())])
        class Source:
            def iter_batches(self, batch_size):
                for start in [0, 4, 8]:
                    yield pa.record_batch([pa.array(range(start, start + 4))], schema=schema)
        def ids(rows, retained=0):
            return [r["id"] for b in source_batches(Source(), schema, 4, rows, retained)
                    for r in b.to_pylist()]
        self.assertEqual(ids(10), list(range(10)))
        self.assertEqual(ids(10, 6), [6, 7, 8, 9])
        self.assertEqual(ids(8, 4), [4, 5, 6, 7])
        self.assertEqual(ids(8, 8), [])

    def test_hot_requires_both_runs(self):
        self.assertEqual(hot([10, 3, 2]), 2)
        self.assertEqual(hot([None, 3, 2]), 2)
        self.assertIsNone(hot([10, None, 2]))

    def test_cross_az_runner_rejected(self):
        placement = {
            "database": {"provider": "aws", "region": "us-east-1", "az": "use1-az1", "evidence": "Cloud API"},
            "runner": {"provider": "aws", "region": "us-east-1", "az": "use1-az2", "evidence": "EC2 API"},
        }
        with self.assertRaisesRegex(ValueError, "verified az"):
            validate_placement(placement)
        placement["runner"]["az"] = "use1-az1"
        self.assertEqual(validate_placement(placement), placement)


if __name__ == "__main__":
    unittest.main()
