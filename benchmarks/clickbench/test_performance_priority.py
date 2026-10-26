import unittest
from performance_priority import api_accepted, api_settled, checkpoint_allowed, priority_authorized


class PerformancePriorityTest(unittest.TestCase):
    def setUp(self):
        self.window = {'requested': True, 'priority': 'performance_before_remaining_api',
                       'authorized_source_thread': 'human-chat',
                       'priority_authorization': {'independently_verified': True,
                          'source_thread': 'human-chat', 'user_message_id': 'human-reply',
                          'answer': 'Run the #406 performance comparison first'}}

    def test_agent_proposal_is_not_authorization(self):
        self.assertTrue(priority_authorized(self.window))
        self.window['priority_authorization']['independently_verified'] = False
        self.assertFalse(priority_authorized(self.window))

    def test_checkpoint_requires_quiescence_and_preserved_evidence(self):
        scan = {'status': 'paused_for_performance', 'checkpoint': 'checkpoint.json',
                'no_active_scan_jobs': True, 'trial_count': 2, 'valid_pairs': 1}
        self.assertTrue(checkpoint_allowed(scan, self.window))
        self.assertFalse(api_accepted(scan))
        for field in ['checkpoint', 'no_active_scan_jobs']:
            broken = dict(scan); broken.pop(field)
            self.assertFalse(checkpoint_allowed(broken, self.window))

    def test_performance_release_is_not_api_completion(self):
        self.assertFalse(api_settled({'released': True, 'results_preserved': True,
                                     'no_active_jobs': True}))
        self.assertFalse(api_settled({'status': 'paused_for_performance', 'trial_count': 2}))

    def test_budget_exhaustion_is_settled_but_not_accepted(self):
        scan = {'status': 'incomplete_budget', 'attempts_complete': True,
                'trial_count': 18, 'no_active_scan_jobs': True}
        self.assertTrue(api_settled(scan)); self.assertFalse(api_accepted(scan))
        scan['no_active_scan_jobs'] = False
        self.assertFalse(api_settled(scan))

class BudgetFailureTests(unittest.TestCase):
    def test_budget_exhaustion_does_not_hide_nonbudget_failure(self):
        from performance_priority import final_api_status
        report={'complete':False,'attempts_complete':True,'trials':[{'error_type':'BudgetDeadline'}], 'comparisons':[{'status':'failed_trial'}]}
        self.assertEqual(final_api_status(report,True),'incomplete_budget')
        for error in ['MemoryLimit','WorkerExit','QueryDeadline']:
            self.assertEqual(final_api_status({**report,'trials':[{'error_type':error}]},True),'failed')
        self.assertEqual(final_api_status({**report,'trials':[{'error_type':'QueryDeadline','deadline_limited':True}]},True),'incomplete_budget')
        self.assertEqual(final_api_status({**report,'comparisons':[{'status':'mismatch'}]},True),'failed')


if __name__ == '__main__':
    unittest.main()
