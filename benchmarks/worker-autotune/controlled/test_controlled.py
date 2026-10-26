# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import copy
import unittest
import json
import tempfile
from pathlib import Path
from gate import POLICIES, evaluate, write_report

class GateTests(unittest.TestCase):
    def fixture(self):
        manifest = dict(repetitions=3, bounds=dict(steady_qps_min=.90, steady_p95_max=1.15, cold_qps_min=.85, cold_p95_max=1.20),
            scenarios=[dict(deployment='oss', rows=32768, cpu=2, condition='direct', mode='waves', concurrencies=[1])])
        records = []
        for repetition in range(3):
            for policy in POLICIES:
                record = dict(deployment='oss', rows=32768, GOMAXPROCS=2, condition='direct', arrival_mode='waves', concurrency=1,
                              repetition=repetition, policy=policy, cold_learning=dict(queries_per_second=10, p95_query_seconds=.1))
                for metric in ['queries_per_second', 'p95_query_seconds', 'client_cpu_seconds', 'client_peak_heap_bytes',
                               'client_process_peak_rss_bytes', 'redis_network_bytes_per_second', 'redis_cpu_seconds', 'redis_memory_bytes']:
                    record[metric] = .1 if metric == 'p95_query_seconds' else 10
                records.append(record)
        return records, manifest

    def test_cancelled_report_preserves_incomplete_coverage(self):
        records, manifest = self.fixture()
        result = evaluate(records[:1], manifest)
        result['status'] = 'cancelled'
        with tempfile.TemporaryDirectory() as directory:
            write_report(directory, result)
            self.assertEqual(json.loads(Path(directory, 'gate.json').read_text())['status'], 'cancelled')
            report = Path(directory, 'REPORT.md').read_text()
            self.assertIn('Incomplete coverage:', report)
            self.assertNotIn('Infrastructure failure:', report)

    def test_pass_and_regression(self):
        records, manifest = self.fixture()
        self.assertEqual(evaluate(records, manifest)['status'], 'pass')
        for record in records:
            if record['policy'] == 'tuned':
                record['queries_per_second'] = 8
        result = evaluate(records, manifest)
        self.assertEqual(result['status'], 'regression')
        self.assertFalse(result['cases'][0]['passed'])

    def test_missing_duplicate_and_cold_failure(self):
        records, manifest = self.fixture()
        self.assertEqual(evaluate(records[:-1], manifest)['status'], 'infrastructure')
        self.assertEqual(evaluate(records + [records[0]], manifest)['status'], 'infrastructure')
        records = copy.deepcopy(records)
        for record in records:
            if record['policy'] == 'tuned':
                record['cold_learning']['p95_query_seconds'] = .13
        self.assertEqual(evaluate(records, manifest)['status'], 'regression')


class DiagnosticCompletenessTests(unittest.TestCase):
    def test_required_legacy_trials(self):
        records, manifest = GateTests.fixture(self)
        manifest['baseline_comparison'] = True
        manifest['scenarios'][0]['cpu'] = 4
        for record in records: record['GOMAXPROCS'] = 4
        self.assertEqual(evaluate(records, manifest)['status'], 'infrastructure')
        legacy = [dict(record, policy='legacy-tuned') for record in records if record['policy'] == 'tuned']
        result = evaluate(records + legacy, manifest)
        self.assertEqual(result['status'], 'pass')
        self.assertEqual(result['cases'][0]['legacy_comparison']['qps_ratio'], 1)

if __name__ == '__main__':
    unittest.main()
