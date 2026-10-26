"""Check independent scan validation and use of the actual public partition API."""
import concurrent.futures
import datetime
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch

import pyarrow as pa
import pyarrow.parquet as pq

from scan_paths import scan_cases
from scan_trial import execute
from scan_validate import validate


class ScanValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.p = Path(self.temp.name)
        fields = [('WatchID', 'BIGINT'), ('UserID', 'BIGINT'), ('RegionID', 'INTEGER'),
                  ('URL', 'TEXT'), ('SearchPhrase', 'TEXT'), ('EventDate', 'DATE'),
                  ('EventTime', 'TIMESTAMP'), ('AdvEngineID', 'SMALLINT')]
        fields += [(f'Filler{i}', 'BIGINT') for i in range(97)]
        self.ddl = self.p / 'create.sql'
        self.ddl.write_text('\n'.join(f'{name} {typ},' for name, typ in fields))
        source = {name: [0] * 4 for name, _ in fields}
        source.update(WatchID=[7, 7, 8, 9], UserID=[10, None, 30, 40], RegionID=[1, 2, 3, 4],
                      URL=['a', '你好', 'c', 'd'], SearchPhrase=['', 'x', None, 'z'],
                      EventDate=[0, 1, 2, 3], EventTime=[0, 1, 2, 3], AdvEngineID=[0, 1, 2, 0])
        self.source = source
        self.parquet = self.p / 'source.parquet'
        pq.write_table(pa.table(source), self.parquet)
        self.sequence = 0

    def record(self, columns, chunks=None):
        table = pa.table(columns)
        chunks = chunks or [list(range(table.num_rows))]
        files = []
        for indices in chunks:
            self.sequence += 1
            path = self.p / f'output-{self.sequence}.arrow'
            partial = table.take(pa.array(indices, type=pa.int64()))
            with pa.OSFile(str(path), 'wb') as stream, pa.ipc.new_stream(stream, table.schema) as writer:
                writer.write_table(partial)
            files.append({'path': str(path), 'rows': partial.num_rows,
                          'schema': table.schema.serialize().to_pybytes().hex()})
        return {'rows': table.num_rows, 'files': files}

    def job(self, ordinary, native, case=None):
        return {'statement': ordinary, 'partitions': native, 'case': case or scan_cases(4)[0],
                'parquet': str(self.parquet), 'ddl': str(self.ddl),
                'temp_directory': str(self.p / 'spill')}

    def test_reordered_partitions_and_duplicate_values_are_exact(self):
        data = {'__rowid': [1, 2, 3, 4], 'watchid': [7, 7, 8, 9]}
        result = validate(self.job(self.record(data), self.record(data, [[2, 0], [3, 1]])))
        self.assertEqual(result['status'], 'valid')
        self.assertTrue(result['exact_cross_api_multiset'])
        self.assertEqual(result['source_oracle']['scope'], 'all returned rows, exact typed-source multiset')

    def test_duplicate_rowid_is_rejected(self):
        original = self.record({'__rowid': [1, 2, 3, 4], 'watchid': [7, 7, 8, 9]})
        bad = self.record({'__rowid': [1, 2, 3, 3], 'watchid': [7, 7, 8, 9]})
        self.assertEqual(validate(self.job(original, bad))['status'], 'mismatch')

    def test_schema_difference_is_rejected(self):
        original = self.record({'__rowid': [1, 2, 3, 4], 'watchid': [7, 7, 8, 9]})
        bad = self.record({'__rowid': [1, 2, 3, 4], 'watchid': ['7', '7', '8', '9']})
        self.assertEqual(validate(self.job(original, bad))['status'], 'mismatch')

    def test_matching_but_wrong_outputs_fail_independent_oracle(self):
        bad = {'__rowid': [1, 2, 3, 4], 'watchid': [7, 7, 8, 666]}
        result = validate(self.job(self.record(bad), self.record(bad)))
        self.assertEqual(result['status'], 'mismatch')
        self.assertEqual(result['source_oracle']['difference_rows'], 1)

    def test_filtered_nullable_results(self):
        data = {'__rowid': [2, 3], 'watchid': [7, 8], 'userid': [None, 30], 'advengineid': [1, 2]}
        result = validate(self.job(self.record(data), self.record(data, [[1], [0]]), scan_cases(4)[1]))
        self.assertEqual(result['status'], 'valid')
        self.assertEqual(result['source_oracle']['expected_rows'], 2)

    def test_bounded_mixed_values_and_typed_source(self):
        data = {'__rowid': [1, 2], 'watchid': [7, 7], 'userid': [10, None], 'regionid': [1, 2],
                'url': ['a', '你好'], 'searchphrase': ['', 'x'],
                'eventdate': [datetime.date(1970, 1, 1), datetime.date(1970, 1, 2)],
                'eventtime': [datetime.datetime(1970, 1, 1), datetime.datetime(1970, 1, 1, 0, 0, 1)]}
        case = {**scan_cases(4)[2], 'expected_rows': 2, 'max_rowid': 2}
        result = validate(self.job(self.record(data), self.record(data, [[1], [0]]), case))
        self.assertEqual(result['status'], 'valid')
        self.assertEqual(result['source_oracle']['difference_rows'], 0)


class PublicApiTests(unittest.TestCase):
    def test_both_paths_use_their_respective_api_and_drain_all_batches(self):
        table = pa.table({'__rowid': [1, 2, 3, 4], 'watchid': [11, 12, 13, 14]})
        calls = []

        class Cursor:
            def __init__(self):
                self.adbc_statement = self
                self.partition = None
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def execute(self, sql): calls.append(('execute', sql))
            def set_options(self, **kwargs): calls.append(('options', kwargs))
            def adbc_execute_partitions(self, sql):
                calls.append(('execute_partitions', sql))
                return [b'0', b'1'], table.schema
            def adbc_read_partition(self, descriptor):
                calls.append(('read_partition', descriptor))
                self.partition = int(descriptor)
            def fetch_record_batch(self):
                data = table if self.partition is None else table.slice(self.partition * 2, 2)
                return data.to_reader(max_chunksize=1)

        class Connection:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def cursor(self): return Cursor()

        class Pool(concurrent.futures.ThreadPoolExecutor):
            def __init__(self, max_workers, mp_context): super().__init__(max_workers=max_workers)

        with tempfile.TemporaryDirectory() as directory:
            job = {'driver': 'test.so', 'namespace': 'clickbench_123456abcdef',
                   'sql': 'SELECT __rowid, watchid FROM hits'}
            with patch('scan_trial.dbapi.connect', return_value=Connection()), \
                    patch('scan_trial.concurrent.futures.ProcessPoolExecutor', Pool):
                ordinary = execute({**job, 'api': 'statement', 'data_directory': directory + '/ordinary'}, 'redis://127.0.0.1')
                self.assertEqual(ordinary['rows'], 4)
                self.assertFalse(any(c[0] == 'execute_partitions' for c in calls))
                calls.clear()
                native = execute({**job, 'api': 'partitions', 'data_directory': directory + '/native'}, 'redis://127.0.0.1')
                self.assertEqual(native['rows'], 4)
                self.assertEqual(native['partitions'], 2)
                self.assertEqual(sum(c[0] == 'execute_partitions' for c in calls), 1)
                self.assertEqual(sum(c[0] == 'read_partition' for c in calls), 2)
                self.assertFalse(any(c == ('execute', job['sql']) for c in calls))


if __name__ == '__main__':
    unittest.main()
