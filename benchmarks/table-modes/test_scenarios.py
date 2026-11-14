import threading
import unittest
from unittest.mock import Mock, patch

import scenarios as s


class ScenarioContracts(unittest.TestCase):
    def test_group_oracle_has_exact_integer_totals_and_types(self):
        actual=s.expected_query('grouped_count_sum',0,10000).to_pylist()
        for row in actual:
            ids=[i for i in range(1,10001) if i%16==row['grp']]
            self.assertEqual(row['n'],len(ids))
            self.assertEqual(row['total'],sum(i*3 for i in ids))
        self.assertEqual(sum(r['n'] for r in actual),10000)

    def test_point_schedule_is_unique_for_mutations(self):
        for op in ('update','delete'):
            ids=[s.point_id(op,i,1000) for i in range(32)]
            self.assertEqual(len(set(ids)),32)
            self.assertTrue(all(1<=i<=1000 for i in ids))
        self.assertTrue(all(s.point_id('insert',i,1000)>1000 for i in range(32)))

    def test_failed_cursor_page_is_closed_and_not_replayed(self):
        r=Mock();pipe=Mock();r.pipeline.return_value.__enter__=Mock(return_value=pipe)
        r.pipeline.return_value.__exit__=Mock(return_value=False)
        pipe.execute.side_effect=TimeoutError('unknown reply')
        r.execute_command.side_effect=[({b'results':[{b'extra_attributes':{b'__key':b'tablemode:source:1'}}]},73),None]
        with self.assertRaises(TimeoutError):s.raw_query(r,{},'filtered_projection',0,10000)
        self.assertEqual(r.execute_command.call_count,2)
        self.assertEqual(r.execute_command.call_args.args,('FT.CURSOR','DEL',s.base.INDEX,73))
        pipe.execute.assert_called_once()

    def test_setup_failure_aborts_waiters_and_is_not_retried(self):
        barriers=[threading.Barrier(2) for _ in range(3)];release=threading.Event()
        with patch.object(s.base,'connect',side_effect=RuntimeError('private text')) as connect:
            result=s.trial_worker({},'managed','point_read',[0],64,{},barriers,release)
        connect.assert_called_once()
        self.assertEqual(result,{'error_type':'RuntimeError','sqlstate':None})
        self.assertTrue(all(b.broken for b in barriers));self.assertTrue(release.is_set())


if __name__=='__main__':unittest.main()
