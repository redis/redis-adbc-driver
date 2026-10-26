import unittest

from validate_local_execution import valid_boundary_ties


class LocalExecutionValidation(unittest.TestCase):
    def test_complete_groups_and_boundary(self):
        full = [("a", 3, 70), ("b", 2, 20), ("c", 2, 21), ("d", 1, 10)]
        expected = full[:2]
        self.assertTrue(valid_boundary_ties([["a", "3", "70"], ["c", "2", "21"]], expected, full, 1))
        for actual in [full[1:3], [full[0], full[0]], [full[2], full[0]],
                       [full[0], full[3]], [("a", 3, 69), full[1]], [full[0]], []]:
            self.assertFalse(valid_boundary_ties(actual, expected, full, 1), actual)

    def test_empty_result(self):
        self.assertTrue(valid_boundary_ties([], [], [], 1))
        self.assertFalse(valid_boundary_ties([["a", 1]], [], [], 1))


if __name__ == "__main__":
    unittest.main()
