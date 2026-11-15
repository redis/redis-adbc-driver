import copy
import unittest

from sparse_audit import audit, SHAPES, MODES


def fixture():
    records = []
    for shape in SHAPES:
        for mode in MODES:
            for clients in (1, 4):
                for repeat in (-1, 0, 1, 2):
                    non_null = {"dense": 256, "sparse90": 26, "all_null": 0, "sparse90_wide": 26}[shape]
                    projected = mode == "projected_fields"
                    records.append(dict(shape=shape, mode=mode, clients=clients, repeat=repeat,
                                        warmup=repeat < 0, validated=True, operations=8, hash_rows=256,
                                        non_null_selected_rows=non_null, wrong_type_keys=1, missing_keys=1,
                                        wide_unselected_bytes=16384 if shape == "sparse90_wide" else 0,
                                        latency_ns=[100] * 8, seconds=.1,
                                        commands={"hmget": 2064 if projected else 0,
                                                  "hgetall": 8 * (257 - non_null) if projected else 2064}))
    return records


class SparseAuditTest(unittest.TestCase):
    def test_complete(self):
        result = audit(fixture())
        self.assertEqual(len(result), 16)
        self.assertTrue(all(row["samples"] == 24 for row in result))

    def test_corruption_is_rejected(self):
        for corruption in ("missing", "duplicate", "fallback", "validation", "nan", "samples"):
            with self.subTest(corruption=corruption):
                rows = copy.deepcopy(fixture())
                if corruption == "missing": rows.pop()
                if corruption == "duplicate": rows[-1] = rows[0]
                if corruption == "fallback": rows[-1]["commands"]["hgetall"] -= 8
                if corruption == "validation": rows[-1]["validated"] = False
                if corruption == "nan": rows[-1]["seconds"] = float("nan")
                if corruption == "samples": rows[-1]["latency_ns"].pop()
                with self.assertRaises(ValueError): audit(rows)


if __name__ == "__main__":
    unittest.main()
