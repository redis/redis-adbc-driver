import unittest

from native_stages import CursorStages


class CursorStageTest(unittest.TestCase):
    def test_same_cursor_results_and_exceptions_are_preserved(self):
        result = object()
        class Cursor:
            rowcount = 1
            connection = object()
            def execute(self, sql):
                self.sql = sql
                return self
            def fetch_arrow_table(self):
                return result
        original = Cursor()
        record = {'stage_ns': {}}
        proxy = CursorStages(original, record)
        self.assertIs(proxy.execute('SELECT synthetic'), original)
        self.assertEqual(original.sql, 'SELECT synthetic')
        self.assertIs(proxy.fetch_arrow_table(), result)
        self.assertEqual(proxy.rowcount, 1)
        self.assertIs(proxy.connection, original.connection)
        self.assertEqual(set(record['stage_ns']), {'execute', 'fetch_arrow_table', 'rowcount'})
        self.assertTrue(all(value > 0 for value in record['stage_ns'].values()))
        self.assertNotIn('SELECT synthetic', str(record))

    def test_failed_call_is_timed_without_replay(self):
        failure = RuntimeError('synthetic refusal')
        class Cursor:
            count = 0
            def execute(self, sql):
                self.count += 1
                raise failure
        cursor = Cursor()
        record = {'stage_ns': {}}
        with self.assertRaises(RuntimeError) as raised:
            CursorStages(cursor, record).execute('SQL excluded from trace')
        self.assertIs(raised.exception, failure)
        self.assertEqual(cursor.count, 1)
        self.assertGreater(record['stage_ns']['execute'], 0)
        self.assertNotIn('SQL excluded', str(record))


if __name__ == '__main__':
    unittest.main()
