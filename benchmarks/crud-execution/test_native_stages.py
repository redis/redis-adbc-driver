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

class NativeSubstageTest(unittest.TestCase):
    def test_nested_callbacks_preserve_actual_result_cancel_and_error(self):
        import types
        import native_stages as stages
        calls = []
        class Result:
            def close(self):
                calls.append('close')
        def blocking(function, args, kwargs, cancel):
            calls.append(cancel)
            return function(*args, **kwargs)
        dbapi = types.SimpleNamespace(_RowIterator=Result, _blocking_call=blocking)
        stages.install_native_substages(dbapi)
        value = object()
        cancel = object()
        record = {'stage_ns': {}}
        stages._active_record.record = record
        try:
            def execute_query():
                return value
            self.assertIs(dbapi._blocking_call(execute_query, (), {}, cancel), value)
            dbapi._RowIterator().close()
            failure = RuntimeError('same failure')
            def prepare():
                raise failure
            with self.assertRaises(RuntimeError) as raised:
                dbapi._blocking_call(prepare, (), {}, cancel)
            self.assertIs(raised.exception, failure)
        finally:
            stages._active_record.record = None
        self.assertEqual(calls, [cancel, 'close', cancel])
        self.assertEqual(set(record['native_stage_ns']),
                         {'adbc_execute_query', 'previous_result_close', 'adbc_prepare'})
        self.assertTrue(all(value > 0 for value in record['native_stage_ns'].values()))
