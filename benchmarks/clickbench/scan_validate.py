"""Exact multiset validation of retained scan outputs, outside query timers."""
import json
from pathlib import Path
import sys

import duckdb
import pyarrow as pa

from references import typed_source
from run import arrow_schema


def output_schema(record):
    schemas = [pa.ipc.read_schema(pa.BufferReader(bytes.fromhex(f['schema'])))
               for f in record['files']]
    if not schemas or any(not schemas[0].equals(s) for s in schemas):
        raise ValueError('Partition output schemas differ')
    return schemas[0]


def register(con, name, record):
    schema = output_schema(record)

    def batches():
        for item in record['files']:
            with pa.ipc.open_stream(item['path']) as stream:
                if not schema.equals(stream.schema):
                    raise ValueError('Retained file schema differs')
                yield from stream

    con.register(name, pa.RecordBatchReader.from_batches(schema, batches()))


def validate(job):
    ordinary, native = job['statement'], job['partitions']
    if not output_schema(ordinary).equals(output_schema(native)):
        return {'status': 'mismatch', 'reason': 'API schemas differ'}
    con = duckdb.connect()
    con.execute("SET memory_limit='12GB'").fetchall()
    con.execute('SET threads=8').fetchall()
    con.execute('SET temp_directory=' + "'" + job['temp_directory'].replace("'", "''") + "'").fetchall()
    con.execute("SET TimeZone='UTC'").fetchall()
    typed_source(con, Path(job['parquet']), arrow_schema(Path(job['ddl']).read_text()))
    con.fetchall()
    stats = {}
    try:
        for name, record in [('ordinary', ordinary), ('native', native)]:
            register(con, name, record)
            count, distinct, minimum, maximum = con.execute(
                f'SELECT count(*), count(DISTINCT __rowid), min(__rowid), max(__rowid) FROM {name}').fetchall()[0]
            stats[name] = {'rows': count, 'unique_rowids': distinct, 'min_rowid': minimum, 'max_rowid': maximum}
            if count != record['rows'] or distinct != count:
                return {'status': 'mismatch', 'reason': 'Missing/duplicate row IDs or inconsistent row count', 'stats': stats}
        if stats['ordinary'] != stats['native']:
            return {'status': 'mismatch', 'reason': 'API row counts or bounds differ', 'stats': stats}
        case = job['case']
        if case['expected_rows'] is not None and stats['ordinary']['rows'] != case['expected_rows']:
            return {'status': 'mismatch', 'reason': 'Unexpected row count', 'stats': stats}
        if case.get('max_rowid') and (stats['ordinary']['min_rowid'] != 1 or stats['ordinary']['max_rowid'] != case['max_rowid']):
            return {'status': 'mismatch', 'reason': 'Expected dense row-ID coverage', 'stats': stats}
        register(con, 'ordinary', ordinary)
        register(con, 'native', native)
        differences = con.execute('SELECT count(*) FROM (SELECT * FROM native EXCEPT ALL SELECT * FROM ordinary)').fetchall()[0][0]
        if differences:
            return {'status': 'mismatch', 'reason': 'API values or multiplicities differ', 'difference_rows': differences, 'stats': stats}
        # Equal counts plus one-way EXCEPT ALL proves full multiset equality.
        register(con, 'ordinary', ordinary)
        cols = ','.join(case['source_columns'])
        if case.get('source_where') is not None:
            expected = con.execute('SELECT count(*) FROM hits ' + case['source_where']).fetchall()[0][0]
            differences = con.execute(f'SELECT count(*) FROM (SELECT {cols} FROM ordinary EXCEPT ALL SELECT {cols} FROM hits {case["source_where"]})').fetchall()[0][0]
            oracle = {'scope': 'all returned rows, exact typed-source multiset', 'expected_rows': expected, 'difference_rows': differences}
            valid = expected == stats['ordinary']['rows'] and differences == 0
        else:
            # Driver row IDs were assigned by concurrent writers and are not source offsets.
            con.execute(f'CREATE TEMP TABLE sample AS SELECT {cols} FROM ordinary LIMIT 100').fetchall()
            differences = con.execute(f'SELECT count(*) FROM (SELECT * FROM sample EXCEPT ALL SELECT {",".join("h."+c for c in case["source_columns"])} FROM hits h JOIN (SELECT DISTINCT watchid FROM sample) s USING(watchid))').fetchall()[0][0]
            oracle = {'scope': '100 source-membership samples; full exact cross-API equality and dense row-ID coverage checked separately', 'difference_rows': differences}
            valid = differences == 0
        return {'status': 'valid' if valid else 'mismatch', 'stats': stats,
                'exact_cross_api_multiset': True, 'source_oracle': oracle}
    finally:
        con.close()


def main():
    job = json.loads(Path(sys.argv[1]).read_text())
    try:
        result = validate(job)
    except Exception as error:
        result = {'status': 'validation_error', 'error_type': type(error).__name__, 'message': str(error)[:500]}
    Path(job['output']).write_text(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
