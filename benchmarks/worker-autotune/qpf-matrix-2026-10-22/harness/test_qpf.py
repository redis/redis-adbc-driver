import unittest

from qpf import verify_qpf


class QPFPreflightTest(unittest.TestCase):
    def test_requires_effective_settings_not_only_requested_factor(self):
        good = dict(status='active', query_performance_factor=dict(active=True, scaling_factor=4),
                    search={'search-workers': 6}, sched_policy='mnp', conns=32)
        self.assertEqual(verify_qpf(good), good)
        for key, value in [('status', 'pending'), ('search', {'search-workers': 0}),
                           ('sched_policy', 'cmp'), ('conns', 5),
                           ('query_performance_factor', dict(active=False, scaling_factor=4))]:
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                verify_qpf(dict(good, **{key: value}))

    def test_standard_and_other_factors(self):
        for factor in [0, 2, 4, 8]:
            good = dict(status='active', query_performance_factor=dict(active=True, scaling_factor=factor),
                        search={'search-workers': factor * 3 // 2},
                        sched_policy='mnp' if factor else 'cmp', conns=32 if factor else 5)
            self.assertEqual(verify_qpf(good, factor), good)
            with self.assertRaises(RuntimeError):
                verify_qpf(dict(good, query_performance_factor=dict(active=False, scaling_factor=factor)), factor)
