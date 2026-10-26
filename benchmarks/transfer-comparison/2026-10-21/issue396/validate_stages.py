"""Validate final diagnostic stage outputs against every independently typed value."""
import argparse
from pathlib import Path
import pyarrow as pa

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('work', type=Path)
parser.add_argument('--stage-dir', default='stages')
args = parser.parse_args()
expected = pa.ipc.open_file(str(args.work / 'source.arrow')).read_all()
order = [(name, 'ascending') for name in expected.column_names]
expected = expected.sort_by(order).combine_chunks()
for strategy in ['load_all', 'discover_hmget']:
    actual = pa.ipc.open_stream(str(args.work / args.stage_dir / (strategy + '.arrow'))).read_all()
    actual = pa.Table.from_arrays(
        [actual.column(i).cast(field.type, safe=True) for i, field in enumerate(expected.schema)],
        schema=expected.schema,
    )
    if not actual.sort_by(order).combine_chunks().equals(expected, check_metadata=False):
        raise AssertionError(strategy + ': full-value/multiplicity mismatch')
    print(strategy, 'EXACT FULL VALUE VALIDATION PASS', actual.num_rows, actual.num_columns)
