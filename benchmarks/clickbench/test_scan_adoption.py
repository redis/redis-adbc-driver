import unittest
from scan_adoption import adopted_record, identify_job


class AdoptionTests(unittest.TestCase):
    def setUp(self):
        self.root = '/opt/clickbench'
        self.name = 'narrow_full-2-statement-resume-original'
        self.path = self.root + '/results/scan-paths-fixed-jobs/' + self.name + '-job.json'
        self.report = {'namespace': 'original', 'cases': [{'name': 'narrow_full', 'sql': 'SELECT x'}]}
        self.job = {'namespace': 'original', 'sql': 'SELECT x', 'api': 'statement',
                    'driver': self.root + '/fix-source/go/build/libadbc_driver_redis.so',
                    'output': self.path.replace('-job.json', '-result.json'),
                    'data_directory': self.root + '/scan-data/fixed/' + self.name}

    def test_original_resumed_identity(self):
        self.assertEqual(identify_job(self.path, self.job, self.report, self.root),
                         {'case': 'narrow_full', 'trial': 2, 'api': 'statement'})

    def test_reject_unrelated_job(self):
        for key, value in [('namespace', 'other'), ('driver', '/other'), ('sql', 'DELETE x'),
                           ('api', 'partitions'), ('output', '/tmp/x'), ('data_directory', '/tmp/x')]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                identify_job(self.path, {**self.job, key: value}, self.report, self.root)

    def test_active_result_cannot_be_adopted(self):
        with self.assertRaises(ValueError):
            adopted_record({}, 'unit', {'ActiveState': 'active'}, {'seconds': 1})

    def test_output_and_failures_remain_distinct(self):
        identity = {'case': 'narrow_full', 'trial': 2, 'api': 'statement'}
        output = {'seconds': 17, 'rows': 99, 'files': [{'path': 'retained'}]}
        record = adopted_record(identity, 'unit', {'Result': 'success'}, output)
        self.assertEqual(record['seconds'], 17)
        self.assertEqual(record['files'], output['files'])
        for result, expected in [('timeout', 'QueryDeadline'), ('oom-kill', 'MemoryLimit'), ('exit-code', 'WorkerExit')]:
            record = adopted_record(identity, 'unit', {'Result': result}, output)
            self.assertIsNone(record['seconds'])
            self.assertEqual(record['error_type'], expected)


if __name__ == '__main__':
    unittest.main()
