"""Native correctness screen for public execute/executemany(None) caller paths.

Requires the same owned Software fixture and gates as scenarios.py. This is not
latency evidence or the complete ACL/cancellation/Close acceptance suite.
"""
import argparse
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'table-modes'))
import scenarios

pa = scenarios.pa
base = scenarios.base


def check(benchmark, mode, execution):
    rows = 1000
    rowids = benchmark.setup(mode, rows)
    table = 'bench.' + mode
    exact = 9007199254740993
    text = 'exact' + chr(0) + 'é'
    assert 0 in text.encode()
    expected = {i: scenarios.record(i) for i in range(1, rows + 1)}
    counts = []
    with base.connect(benchmark.cfg) as c, c.cursor() as q:
        def mutation(sql, parameters=None):
            if execution == 'execute':
                q.execute(sql, parameters)
            else:
                q.executemany(sql, parameters)
            counts.append(q.rowcount)
        key = base.PREFIX + '2000'
        names = list(scenarios.SCHEMA.names) + [base.ORDINAL]
        data = [exact, None, -exact, text, 2000]
        schema = pa.schema(list(scenarios.SCHEMA) + [pa.field(base.ORDINAL, pa.int64())])
        if mode == 'unmanaged':
            names.insert(0, '__key'); data.insert(0, key)
            schema = pa.schema([pa.field('__key', pa.string()), *schema])
        parameters = pa.RecordBatch.from_arrays([pa.array([v], type=f.type) for f, v in zip(schema, data)], names=names)
        mutation(f'INSERT INTO {table} (' + ','.join(names) + ') VALUES (' + ','.join('?' for _ in names) + ')', parameters)
        expected[2000] = dict(id=exact, grp=None, amount=-exact, label=text)
        def predicate(i):
            return f'__rowid={rowids[i]}' if mode == 'managed' else '__key=' + base.literal(base.PREFIX + str(i))
        mutation(f'UPDATE {table} SET amount=?, label=? WHERE {predicate(1)}',
                 pa.record_batch([pa.array([-exact], pa.int64()), pa.array([None], pa.string())], names=['amount', 'label']))
        expected[1].update(amount=-exact, label=None)
        mutation(f'DELETE FROM {table} WHERE {predicate(2)}')
        del expected[2]
        mutation(f'UPDATE {table} SET amount=1 WHERE id=-1')
        assert counts == [1, 1, 1, 0], counts
        # Reuse this cursor across mutations, normal SELECT, then another
        # mutation. The public manager owns preceding-stream cleanup.
        q.execute(f'SELECT id,grp,amount,label FROM {table} ORDER BY id')
        actual = q.fetch_arrow_table()
        want = pa.Table.from_pylist(list(expected.values()), schema=scenarios.SCHEMA).sort_by([('id', 'ascending')])
        assert actual.equals(want, check_metadata=False)
        mutation(f'UPDATE {table} SET amount=amount WHERE id=-1')
        assert counts[-1] == 0
        failures = []
        for sql in ('INVALID SQL', f'UPDATE {table} SET missing_column=1'):
            try:
                mutation(sql)
            except Exception as error:
                state = getattr(error, 'sqlstate', None)
                if isinstance(state, bytes): state = state.decode()
                failures.append(dict(type=type(error).__name__, sqlstate=state,
                                     status=str(getattr(error, 'status_code', None))))
            else:
                raise AssertionError('Invalid mutation succeeded')
    if mode == 'unmanaged':
        with base.client(benchmark.cfg) as r:
            keys = set(r.scan_iter(match=base.PREFIX + '*'))
            assert keys == {(base.PREFIX + str(i)).encode() for i in expected}
            for i, value in expected.items():
                raw = r.hgetall(base.PREFIX + str(i))
                for field in scenarios.SCHEMA:
                    v = value[field.name]
                    actual = raw.get(field.name.encode())
                    want = None if v is None else str(v).encode()
                    assert actual == want, (i, field.name)
    return dict(mode=mode, execution=execution, affected_rows=counts,
                exact_values_and_nulls=True, complete_state_validated=True,
                raw_source_validated=mode == 'unmanaged', errors=failures)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('driver', 'cli', 'work'): p.add_argument('--' + name, type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--dedicated-empty-server', action='store_true', required=True)
    args = p.parse_args()
    # Reuse actual Software/TLS/RESP3 and owned-empty fixture gates. No timings.
    args.suite = 'all'; args.repeats = 3; args.operations = 32
    args.queries = 8; args.point_rows = 1000; args.olap_rows = 100000
    benchmark = scenarios.ScenarioBenchmark(args)
    for mode in ('managed', 'unmanaged'):
        cases = [check(benchmark, mode, path) for path in ('execute', 'executemany')]
        assert cases[0]['errors'] == cases[1]['errors']
        benchmark.results.setdefault('caller_checks', []).extend(cases)
        benchmark.save()
    benchmark.results['status'] = 'correctness_screen_complete'
    benchmark.results['final_qpf'] = benchmark.gate()
    benchmark.save()


if __name__ == '__main__':
    main()
