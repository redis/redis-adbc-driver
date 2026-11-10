"""Keep triage completion distinct from compatibility success."""
import copy
import unittest

import check_issue359 as gate


class Issue359GateTests(unittest.TestCase):
    def setUp(self):
        self.inventory = {'cases': [dict(file='test.json', id=str(i),
            classification='Core-related', classification_note='Related feature; not certification',
            root_cause='test gap', evidence='PostgreSQL 18 comparison',
            disposition='deferred', reason='Requires design',
            owner='https://github.com/redis/redis-adbc-driver/issues/361') for i in range(188)]}
        self.report = {'reference': {'postgres': 'PostgreSQL 18.6'},
            'infrastructure_errors': [], 'results': [dict(file='test.json', id=str(i), status='pass') for i in range(188)]}

    def test_pass_requires_every_observed_pass(self):
        self.assertEqual(gate.check(self.report, self.inventory), [])
        for status in ('fail', 'allowed', 'unsupported'):
            self.report['results'][0]['status'] = status
            self.assertEqual(gate.check(self.report, self.inventory), [('test.json', '0', status)])

    def test_triage_does_not_turn_failure_into_pass(self):
        self.report['results'][0]['status'] = 'fail'
        original = copy.deepcopy(self.report)
        self.assertEqual(len(gate.audit(self.report, self.inventory)), 1)
        self.assertEqual(self.report, original)
        self.assertEqual(len(gate.check(self.report, self.inventory)), 1)
        self.inventory['cases'][0].pop('reason')
        with self.assertRaisesRegex(ValueError, 'undisposed'):
            gate.audit(self.report, self.inventory)

    def test_unobserved_case_cannot_be_disposed(self):
        self.report['results'].pop()
        with self.assertRaisesRegex(ValueError, 'unobserved'):
            gate.audit(self.report, self.inventory)

    def test_wrong_reference_or_infrastructure_is_not_completion(self):
        self.report['reference']['postgres'] = 'PostgreSQL 17.0'
        with self.assertRaisesRegex(ValueError, '18'):
            gate.audit(self.report, self.inventory)
        self.report['reference']['postgres'] = 'PostgreSQL 18.6'
        self.report['infrastructure_errors'] = ['connection failed']
        with self.assertRaisesRegex(ValueError, 'infrastructure'):
            gate.audit(self.report, self.inventory)

    def test_duplicates_and_missing_classification_rejected(self):
        self.report['results'].append(self.report['results'][0])
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            gate.check(self.report, self.inventory)
        self.report['results'].pop()
        self.inventory['cases'][0].pop('classification')
        with self.assertRaisesRegex(ValueError, 'unclassified'):
            gate.audit(self.report, self.inventory)
