"""Independent result and full-run gate regressions; no Redis needed."""
import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('aggregate406',Path(__file__).with_name('compare.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class Comparison(unittest.TestCase):
    def test_exact_integer_and_final_float_rounding(self):
        self.assertTrue(module.validate(2,[[9007199254740993]],[[9007199254740993]]))
        self.assertFalse(module.validate(2,[[9007199254740992]],[[9007199254740993]]))
        self.assertTrue(module.validate(4,[[0.1]],[[0.1]]))
        self.assertFalse(module.validate(4,[[float.fromhex('0x1.999999999999bp-4')]],[[0.1]]))

    def test_global_distinct_rank_and_tied_membership(self):
        expected=[[1,10],[2,8],[3,8]]
        self.assertTrue(module.validate(9,expected,expected))
        self.assertFalse(module.validate(9,[[1,10],[1,10],[2,8]],expected))
        self.assertFalse(module.validate(9,[[2,8],[1,10],[3,8]],expected))
        groups=[[i,20 if i<9 else 10] for i in range(12)]
        self.assertTrue(module.validate(9,groups[:9]+[groups[11]],groups))
        self.assertFalse(module.validate(9,groups[1:11],groups))

    def test_mixed_aggregates_need_complete_values_and_count_ranks(self):
        expected=[[1,3,20,0.5,9],[2,7,10,0.25,8]]
        self.assertTrue(module.validate(10,expected,expected))
        self.assertFalse(module.validate(10,[[1,3,20,0.5,10],expected[1]],expected))

    def test_full_gate_requires_restore_ownership_deadline_and_unreleased_window(self):
        gate={'run':'albert-clickbench-c6a-full-20261021','namespace':'fixture',
              'ready':True,'backup_complete':True,'restore_tested':True}
        module.check_full_window(gate,'fixture',module.CUTOFF,module.CUTOFF-3600)
        for bad in [{'ready':False},{'backup_complete':False},{'restore_tested':False},
                    {'released':True},{'namespace':'other'},{'run':'other'},{'ready':'true'},{'restore_tested':'false'}]:
            with self.assertRaises(ValueError):
                module.check_full_window({**gate,**bad},'fixture',module.CUTOFF,module.CUTOFF-3600)
        for deadline in [None,float('nan'),module.CUTOFF+1,module.CUTOFF-3601]:
            with self.assertRaises(ValueError):
                module.check_full_window(gate,'fixture',deadline,module.CUTOFF-3600)

if __name__=='__main__':unittest.main()
