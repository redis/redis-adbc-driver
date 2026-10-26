import copy
import unittest
from scan_resume import check_resume, reusable, replace_measurement


class ScanResumeTests(unittest.TestCase):
    def setUp(self):
        self.meta={'namespace':'test','loaded_rows':99,'driver_binary_sha256':'binary','placement':{'same':True}}
        self.cases=[{'name':'narrow'}]
        self.good={'case':'narrow','trial':1,'api':'partitions','seconds':2.5,'files':[{'path':'retained'}]}
        self.report={**self.meta,'cases':self.cases,'trials':[dict(self.good)]}

    def test_success_retained_after_other_api_timeout(self):
        self.assertTrue(reusable(self.good, {'status':'failed_trial'}))
        self.assertTrue(reusable({**self.good,'seconds':None,'measured_seconds':2.5}, {'status':'failed_trial'}))

    def test_failed_invalid_or_mismatched_execution_must_retry(self):
        for changes in [{'error_type':'QueryDeadline'},{'seconds':float('nan')},{'seconds':0},{'files':[]}]:
            self.assertFalse(reusable({**self.good,**changes},None))
        self.assertFalse(reusable(self.good,{'status':'mismatch'}))

    def test_identity_and_source_must_match(self):
        check_resume(self.report,{'metadata':self.meta},self.cases)
        for changes in [{'driver_binary_sha256':'different'},{'loaded_rows':100},
                        {'trials':[self.good,self.good]}]:
            with self.assertRaises(ValueError):
                check_resume({**self.report,**changes},{'metadata':self.meta},self.cases)

    def test_failed_attempt_preserved_without_duplicate_final_identity(self):
        report=copy.deepcopy(self.report);report['trials'][0].update(seconds=None,error_type='QueryDeadline')
        replace_measurement(report,self.good)
        self.assertEqual(report['trials'],[self.good])
        self.assertEqual(report['prior_attempts'][0]['error_type'],'QueryDeadline')


if __name__=='__main__':
    unittest.main()
