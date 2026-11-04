"""Build an independent typed reference for a parallel-loaded restored fixture.

Native HASH reads determine membership only. All 105 expected values come from
the checksum-verified official Parquet and independently typed PostgreSQL DDL.
Neither driver output is used as the reference.
"""
import argparse
import hashlib
import json
from pathlib import Path
import sys

import pyarrow as pa


def build_reference(client, namespace, parquet, schema, typed_source, tempdir, expected_sha256):
    parquet = Path(parquet)
    with parquet.open('rb') as source:
        digest = hashlib.file_digest(source, 'sha256').hexdigest()
    if digest != expected_sha256:
        raise ValueError('Official source Parquet identity differs')
    metadata = json.loads(client.get('adbc:{meta}:table:' + namespace + ':hits'))
    prefix = metadata.get('key_prefix') or namespace + ':hits:'
    column = next(c for c in metadata['columns'] if c['name'].lower() == 'watchid')
    field = column.get('field') or column['name']
    watchids = []
    for start in range(1, 100001, 4096):
        end = min(start + 4096, 100001)
        pipe = client.pipeline(transaction=False)
        for internal_id in range(start, end):
            pipe.hmget(prefix + str(internal_id), '__rowid', field)
        rows = pipe.execute()
        if len(rows) != end-start:
            raise ValueError('Incomplete native membership response')
        for internal_id, row in zip(range(start, end), rows):
            if row[0] is None or int(row[0]) != internal_id or row[1] is None:
                raise ValueError('Restored native membership is incomplete or inconsistent')
            watchids.append(int(row[1]))
    if len(watchids) != 100000 or len(set(watchids)) != 100000:
        raise ValueError('Reference membership requires 100K unique WatchIDs')
    import duckdb
    tempdir = Path(tempdir)
    tempdir.mkdir(parents=True, exist_ok=True)
    with duckdb.connect() as con:
        con.execute("SET memory_limit='12GB'")
        con.execute("SET temp_directory='" + str(tempdir).replace("'", "''") + "'")
        typed_source(con, parquet, schema)
        con.register('selected_members', pa.table({'WatchID': pa.array(watchids, type=pa.int64())}))
        result = con.execute('SELECT h.* FROM hits h JOIN selected_members s ON h.WatchID=s.WatchID').to_arrow_table()
    if result.num_rows != 100000 or result.num_columns != 105:
        raise ValueError('Official source membership count differs')
    actual_ids = result.column(next(n for n in result.column_names if n.lower() == 'watchid')).to_pylist()
    if len(set(actual_ids)) != 100000 or set(actual_ids) != set(watchids):
        raise ValueError('Official source membership/multiplicity differs')
    table = pa.Table.from_arrays([result.column(i).cast(field.type, safe=True)
                                 for i, field in enumerate(schema)], schema=schema)
    table = table.rename_columns([name.lower() for name in schema.names])
    membership_digest = hashlib.sha256('\n'.join(map(str, watchids)).encode()).hexdigest()
    return table, {'source_parquet_sha256': digest, 'native_membership_sha256': membership_digest,
                   'rows': 100000, 'columns': 105,
                   'membership': 'Native exact row IDs 1..100000 and unique WatchIDs; every expected value comes from typed official Parquet; not a Parquet-prefix assumption'}


if __name__ == '__main__':
    import redis
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--config', type=Path, required=True)
    ap.add_argument('--parquet', type=Path, required=True)
    ap.add_argument('--parquet-sha256', required=True)
    ap.add_argument('--ddl', type=Path, required=True)
    ap.add_argument('--helpers', type=Path, default=Path(__file__).resolve().parents[1]/'clickbench')
    ap.add_argument('--output', type=Path, required=True)
    args = ap.parse_args()
    sys.path.insert(0, str(args.helpers))
    from run import arrow_schema
    from references import typed_source
    cfg = json.loads(args.config.read_text())
    schema = arrow_schema(args.ddl.read_text())
    with redis.Redis.from_url(cfg['uri'], ssl_ca_certs=cfg['ca'], protocol=3) as client:
        table, proof = build_reference(client, cfg['namespace'], args.parquet, schema,
                                       typed_source, args.output.parent/'duckdb-tmp', args.parquet_sha256)
    with pa.OSFile(str(args.output), 'wb') as sink, pa.ipc.new_file(sink, table.schema) as writer:
        writer.write_table(table)
    print(json.dumps(proof, indent=2))
