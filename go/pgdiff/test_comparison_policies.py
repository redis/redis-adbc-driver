"""Negative controls for the seven source-bound #364 dispositions."""
import copy
import json
from pathlib import Path
import unittest

import run
from comparison_policies import policy_for


class PolicyTests(unittest.TestCase):
    identity = {"postgres_version": "18.6", "postgres_version_num": 180006,
                "postgres_build": "PostgreSQL 18.6 on x86_64, compiled by gcc, 64-bit",
                "postgres_user": "postgres", "redis_user": "default"}

    def case(self, file, ident):
        corpus = json.loads((Path(__file__).parent / 'corpus' / file).read_text())
        return next(c for c in corpus['cases'] if c['id'] == ident)

    def classify(self, file, ident, pg, rd):
        case = self.case(file, ident)
        return run.classify(pg, rd, case, policy=policy_for(file, case), identities=self.identity)

    def test_identity_contracts_reject_wrong_values_types_and_counts(self):
        examples = {
            'row-1': ([['PostgreSQL 18.6 on x86_64, compiled by gcc, 64-bit']], [['PostgreSQL 18.0 (Redis ADBC driver)']]),
            'row-2': ([['PostgreSQL 18.6 on x86_64, compiled by gcc, 64-bit']], [['PostgreSQL 18.0 (Redis ADBC driver)']]),
            'row-6': ([['postgres', 'postgres']], [['default', 'default']]),
            'row-9': ([['18.6']], [['18.0']]),
            'row-10': ([['180006']], [['180000']]),
        }
        for ident, (pgrows, rdrows) in examples.items():
            pg = {'types': ['string'] * len(pgrows[0]), 'rows': pgrows}
            rd = {'types': ['string'] * len(rdrows[0]), 'rows': rdrows}
            with self.subTest(ident=ident):
                self.assertEqual(self.classify('issue-246.json', ident, pg, rd), 'context')
                for engine in ('postgres', 'redis'):
                    for mutation in ('value', 'null', 'type', 'duplicate', 'empty', 'error'):
                        changed = copy.deepcopy(pg if engine == 'postgres' else rd)
                        if mutation == 'value': changed['rows'][0][0] = 'wrong'
                        if mutation == 'null': changed['rows'][0][0] = None
                        if mutation == 'type': changed['types'][0] = 'int64'
                        if mutation == 'duplicate': changed['rows'] *= 2
                        if mutation == 'empty': changed['rows'] = []
                        if mutation == 'error': changed = {'sqlstate': '42601'}
                        self.assertEqual(self.classify('issue-246.json', ident,
                            changed if engine == 'postgres' else pg,
                            changed if engine == 'redis' else rd), 'fail', (engine, mutation))
        # Matching forged values must not fall through to ordinary equality.
        fake = {'types': ['string'], 'rows': [['wrong']]}
        self.assertEqual(self.classify('issue-246.json', 'row-9', fake, fake), 'fail')
        with self.subTest('different endpoint version'):
            self.assertEqual(self.classify('issue-246.json', 'row-1',
                {'types':['string'], 'rows':[['PostgreSQL 18.7 on x86_64, compiled by gcc, 64-bit']]},
                {'types':['string'], 'rows':[['PostgreSQL 18.0 (Redis ADBC driver)']]}), 'fail')

    def test_extension_is_exact_and_is_not_a_dialect_pass(self):
        pg = {'sqlstate': '42601', 'message': 'reference syntax error'}
        rd = {'types': ['timestamptz'], 'rows': [['2024-01-02T00:00:00+00:00']]}
        self.assertEqual(self.classify('issue-268.json', 'repro-4', pg, rd), 'extension')
        for bad in ({'sqlstate':'42883'}, {'sqlstate':'MISSING'}, {'types':[], 'rows':[]}):
            self.assertEqual(self.classify('issue-268.json', 'repro-4', bad, rd), 'fail')
        for bad in ({**rd, 'rows':[['2024-01-03T00:00:00']]},
                    {**rd, 'types':['string']}, {**rd, 'rows':rd['rows'] * 2},
                    {'sqlstate':'42601'}):
            self.assertEqual(self.classify('issue-268.json', 'repro-4', pg, bad), 'fail')

    def test_peers_only_permute_within_contiguous_groups(self):
        pg = {'types':['string','int64','int32','int64'],
              'rows':[['a',1,10,1],['b',2,20,1],['b',2,20,1],['c',3,30,2]]}
        rd = copy.deepcopy(pg)
        rd['rows'] = [pg['rows'][1], pg['rows'][0], pg['rows'][2], pg['rows'][3]]
        check = lambda x: self.classify('postgres/window.json', 'statement-0006', pg, x)
        self.assertEqual(check(rd), 'pass')
        for rows in (list(reversed(pg['rows'])), pg['rows'][:-1],
                     [pg['rows'][0], pg['rows'][1], pg['rows'][3], pg['rows'][2]],
                     [pg['rows'][0]] * 3 + [pg['rows'][3]]):
            self.assertEqual(check({**pg, 'rows': rows}), 'fail')
        rd = copy.deepcopy(pg); rd['rows'][0][2] = 11
        self.assertEqual(check(rd), 'fail')
        self.assertEqual(check({**pg, 'types':['string','int64','int64','int64']}), 'fail')
        self.assertEqual(check({'decode_error':'bad'}), 'fail')

    def test_policy_is_bound_to_exact_source_query(self):
        case = self.case('postgres/window.json', 'statement-0006')
        self.assertIsNone(policy_for('other.json', case))
        with self.assertRaisesRegex(ValueError, 'source SQL changed'):
            policy_for('postgres/window.json', {**case, 'sql':case['sql'] + ' LIMIT 1'})

    def test_dispositions_remain_outside_dialect_denominator(self):
        summary = run.summarize([{'status':s} for s in ['pass','fail','context','extension']])
        self.assertEqual(summary['scored'], 2)
        self.assertEqual(summary['counts'], {'pass':1,'fail':1,'context':1,'extension':1})
