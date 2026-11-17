"""Build independent PostgreSQL18 FACTORIAL comparisons outside the curated corpus."""
import argparse
import json
from pathlib import Path


def corpus():
    cases = []
    def add(expr, suffix='', outside=False):
        case = {'id': f'factorial-{len(cases):03d}', 'sql': f'SELECT {expr}{suffix}'}
        if outside:
            case['unsupported'] = {
                'reason': 'PostgreSQL factorial result exceeds existing Arrow decimal256 precision76; this is not an equivalence pass.',
                'sqlstate': '22003',
            }
        cases.append(case)
    for n in range(57):
        add(f'factorial({n})')
    for argument in ('5::smallint', '5::integer', '5::bigint', "'5'", "' 5 '", "'+5'", "'0x5'", "'5_0'",
                     'NULL', 'NULL::smallint', 'NULL::integer', 'NULL::bigint',
                     '-1', '-9223372036854775808', '32178', '9223372036854775807',
                     "'bad'", "'1.0'", "'9223372036854775808'", '9223372036854775808',
                     '1.0', '1::numeric', '1::float4', '1::float8', "'5'::text", 'true',
                     'NULL::numeric', 'NULL::float8', 'NULL::text', 'ARRAY[5]'):
        for suffix in ('', ' WHERE FALSE'):
            add(f'factorial({argument})', suffix)
    for n in (57, 100):
        for suffix in ('', ' WHERE FALSE'):
            add(f'factorial({n})', suffix, outside=True)
    for expr in ('factorial()', 'factorial(1,2)', 'factorial(NULL::text) IS NULL',
                 "CAST(factorial(NULL::float8) AS TEXT)",
                 "CASE WHEN FALSE THEN factorial('bad') ELSE 1 END",
                 'CASE WHEN FALSE THEN factorial(-1) ELSE 1 END',
                 'coalesce(1,factorial(-1))', 'pg_catalog.factorial(5)',
                 'factorial(factorial(5))'):
        add(expr)
    return {'area': 'factorial', 'setup': [], 'cases': cases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'factorial.json').write_text(json.dumps(corpus(), indent=2) + '\n')


if __name__ == '__main__':
    main()
