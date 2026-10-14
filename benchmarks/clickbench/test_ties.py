import unittest

import duckdb

from ties import check_ties
from validate import same_value


class Ties(unittest.TestCase):
    def setUp(self):
        self.con = duckdb.connect()
        self.con.execute("CREATE TABLE hits (k VARCHAR, n INTEGER, SearchPhrase VARCHAR, EventTime INTEGER)")
        self.con.execute("INSERT INTO hits VALUES ('a',5,'first',1), ('b',4,'x',2), ('c',4,'y',2), ('d',4,'z',2), ('e',3,'last',3)")

    def tearDown(self):
        self.con.close()

    def check(self, sql, rows):
        return check_ties(self.con, sql, rows, same_value)

    def test_ordered_alternative_ties_and_cutoff(self):
        sql = 'SELECT k,n FROM hits ORDER BY n DESC LIMIT 2;'
        self.assertEqual(self.check(sql, [['a',5], ['d',4]]), 'valid')
        for rows in [[['a',5],['e',3]], [['b',4],['c',4]], [['a',5],['d',99]], [['missing',5],['b',4]], [['d',4],['a',5]]]:
            self.assertEqual(self.check(sql, rows), 'mismatch', rows)

    def test_offset_boundaries_and_multiplicity(self):
        sql = 'SELECT k,n FROM hits ORDER BY n DESC LIMIT 2 OFFSET 2;'
        self.assertEqual(self.check(sql, [['b',4],['d',4]]), 'valid')
        self.assertEqual(self.check(sql, [['b',4],['b',4]]), 'mismatch')
        self.assertEqual(self.check(sql, [['a',5],['b',4]]), 'mismatch')
        self.assertEqual(self.check(sql, [['b',4],['e',3]]), 'mismatch')
        sql = 'SELECT k,n FROM hits ORDER BY n DESC LIMIT 4 OFFSET 1;'
        self.assertEqual(self.check(sql, [['d',4],['b',4],['c',4],['e',3]]), 'valid')

    def test_hidden_sort_column(self):
        sql = "SELECT SearchPhrase FROM hits WHERE SearchPhrase <> '' ORDER BY EventTime LIMIT 2;"
        self.assertEqual(self.check(sql, [['first'],['z']]), 'valid')
        self.assertEqual(self.check(sql, [['first'],['last']]), 'mismatch')
        self.assertEqual(self.check(sql, [['x'],['y']]), 'mismatch')

    def test_unordered_groups_require_exact_independent_aggregates(self):
        sql = 'SELECT k,COUNT(*),SUM(n),AVG(n) FROM hits GROUP BY k LIMIT 2;'
        self.assertEqual(self.check(sql, [['d',1,4,4.0],['e',1,3,3.0]]), 'valid')
        for rows in [[['d',2,4,4.0],['e',1,3,3.0]], [['d','invalid',4,4.0],['e',1,3,3.0]], [['d',1.1,4,4.0],['e',1,3,3.0]], [['d',1,5,4.0],['e',1,3,3.0]], [['d',1,4,5.0],['e',1,3,3.0]], [['d',1,4,4.0]] * 2]:
            self.assertEqual(self.check(sql, rows), 'mismatch')

    def test_date_group_keys_and_aggregate_values(self):
        self.con.execute("CREATE TABLE events AS SELECT * FROM (VALUES (1, DATE '2020-01-01', 3), (2, DATE '2020-01-02', 3), (3, DATE '2020-01-03', 2)) AS t(k,d,n)")
        sql = 'SELECT k,d,SUM(n) AS c,AVG(n) AS avg_n FROM events GROUP BY k,d ORDER BY c DESC LIMIT 1;'
        self.assertEqual(self.check(sql, [[2,'2020-01-02',3,3.0]]), 'valid')
        self.assertEqual(self.check(sql, [[2,'2020-01-02',3,9.0]]), 'mismatch')
        self.assertEqual(self.check(sql, [[2,'2020-01-09',3,3.0]]), 'mismatch')

    def test_empty_and_unsupported(self):
        self.assertEqual(self.check('SELECT k FROM hits ORDER BY n DESC LIMIT 2 OFFSET 99;', []), 'valid')
        self.assertEqual(self.check('SELECT k,n FROM hits ORDER BY n DESC,k LIMIT 2;', [['a',5],['b',4]]), 'needs_tie_validation')
        self.assertEqual(self.check('SELECT k,n FROM hits LIMIT 2;', [['a']]), 'mismatch')


if __name__ == '__main__':
    unittest.main()
