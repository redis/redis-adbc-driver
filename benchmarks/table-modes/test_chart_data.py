import copy
import csv
import itertools
import json
from pathlib import Path
import statistics
import tempfile
import unittest

import chart_data as c

TABLE=json.loads((Path(__file__).parent/'merged-scoped-2026-11-13/measurements.json').read_text())


def evidence():
    metadata={k:TABLE['metadata'][k] for k in ('driver_source_commit','driver_version','driver_sha256')}
    metadata.update(suite='all',automatic_retries=0,tls=True,protocol=3,repetitions=1,operations=4,queries=4,point_rows=64,olap_rows=10000)
    data={'metadata':metadata,'status':'complete','failures':[],
          'final_qpf':{'factor':4,'workers':6,'routing':'mnp','connections':32,'memory_bytes':4294967296},'trials':[]}
    for suite,ops in [('oltp',c.POINT),('olap',c.QUERIES)]:
        for op,mode,n,rep in itertools.product(ops,c.MODES,(1,4),(-1,0)):
            values=[100000,200000,300000,400000]
            data['trials'].append({'suite':suite,'operation':op,'mode':mode,'clients':n,'repeat':rep,'warmup':rep<0,
                'fixture_rows':metadata['point_rows' if suite=='oltp' else 'olap_rows'],'columns':4,'operations':4,
                'status':'validated','validated':True,'seconds':1.,'latency_ns':values,
                'median_latency_ms':statistics.median(values)/1e6,'p95_latency_ms':.4,'operations_per_second':4.})
    return data


class ChartAudit(unittest.TestCase):
    def test_all_charts_have_native_baselines_and_explicit_units(self):
        doc=c.make_charts(TABLE,evidence())
        self.assertEqual(len(doc['charts']['slide4_overview']),9)
        for chart,rows in doc['charts'].items():
            for row in rows:
                self.assertIn(row['unit'],('ms','s'))
                self.assertEqual(row['elapsed_multiple'],row['value']/row['redis_py_value'])
                if row['mode']=='redis_py':self.assertEqual(row['elapsed_multiple'],1.)
        self.assertEqual({r['unit'] for r in doc['charts']['oltp']},{'ms'})
        self.assertEqual({r['unit'] for r in doc['charts']['bulk_ingestion']},{'s'})
        with tempfile.TemporaryDirectory() as directory:
            out=Path(directory)/'charts';count=c.write_outputs(doc,out)
            with (out/'charts.csv').open() as f:self.assertEqual(len(list(csv.DictReader(f))),count)
            self.assertEqual(json.loads((out/'charts.json').read_text()),doc)

    def test_missing_duplicate_failed_and_wrong_percentile_samples_rejected(self):
        changes=[lambda d:d['trials'].pop(),lambda d:d['trials'].append(copy.deepcopy(d['trials'][0])),
                 lambda d:d['trials'][0].update(validated=False),lambda d:d['trials'][0].update(p95_latency_ms=99)]
        for change in changes:
            with self.subTest(change=change):
                data=evidence();change(data)
                with self.assertRaises(AssertionError):c.scenario_summary(data)

    def test_matching_future_driver_identity_is_supported(self):
        data=evidence();table=copy.deepcopy(TABLE)
        for m in (data['metadata'],table['metadata']):
            m['driver_source_commit']='a'*40;m['driver_sha256']='b'*64;m['driver_version']='v9.0.0'
        self.assertEqual(c.make_charts(table,data)['driver']['driver_version'],'v9.0.0')

    def test_build_mismatch_rejected(self):
        data=evidence();data['metadata']['driver_sha256']='different'
        with self.assertRaises(AssertionError):c.make_charts(TABLE,data)

    def test_historical_aggregate_series_has_separate_metric_and_csv(self):
        d=json.loads((Path(__file__).parents[1]/'exact-aggregate-406/2026-10-21/full/summary.json').read_text())
        doc=c.make_charts(TABLE,evidence());c.add_historical_aggregates(doc,d)
        self.assertEqual(len(doc['historical_aggregate_comparison']['values']),8)
        self.assertIn('Median',doc['historical_aggregate_comparison']['metric'])
        with tempfile.TemporaryDirectory() as directory:
            out=Path(directory)/'charts';c.write_outputs(doc,out)
            with (out/'aggregate-historical.csv').open() as f:self.assertEqual(len(list(csv.DictReader(f))),8)

    def test_historical_product_data_is_separate(self):
        data=json.loads((Path(__file__).parents[1]/'clickbench/full-c6a-cluster-2026-10-21/early-comparison/comparison.json').read_text())
        doc=c.make_charts(TABLE,evidence());c.add_historical(doc,data)
        self.assertTrue(doc['historical_clickbench']['historical'])
        self.assertIn('Unequal resources',doc['historical_clickbench']['hardware'])
        self.assertEqual(len(doc['historical_clickbench']['values']),44)
        self.assertNotIn('clickbench',doc['charts'])


if __name__=='__main__':unittest.main()
