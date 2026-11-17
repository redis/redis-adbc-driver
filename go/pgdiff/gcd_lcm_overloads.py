"""Build the independent #475 type/literal matrix outside the curated corpus.

Run: python go/pgdiff/gcd_lcm_overloads.py /private/output/directory
Then compare that directory with pgdiff's --corpus option.
"""
import argparse
import json
from pathlib import Path


def corpus():
    values = ['12::smallint', '18::integer', '24::bigint',
              '1.20::numeric(8,2)', '1::real', '1::float8',
              "'12'::text", 'true', "'18'", 'NULL']
    types = ['smallint', 'integer', 'bigint', 'numeric(8,2)',
             'real', 'float8', 'text', 'boolean']
    literals = ["NULL,NULL", "'12','18'", "1::smallint,2::smallint",
                "NULL::text,1::integer", "NULL::numeric,true",
                "NULL::integer,'bad'", "1::integer,'2147483648'",
                "1.2::numeric(5,1),'0.450'", "1.2::numeric(5,1),'0x10'",
                "1.2::numeric(5,1),'1_000.250'",
                "1::bigint,'9223372036854775808'", "1::numeric(76,0),'1e-76'"]
    cases = []
    for function in ('gcd', 'lcm'):
        for i, left in enumerate(values):
            for j, right in enumerate(values):
                cases.append({'id': f'{function}-{i}-{j}',
                              'sql': f'SELECT {function}({left},{right})'})
        for i, typ in enumerate(types):
            for j, right in enumerate(values):
                cases.append({'id': f'{function}-null-{i}-{j}',
                              'sql': f'SELECT {function}(NULL::{typ},{right})'})
        for i, args in enumerate(literals):
            for kind, suffix in (('empty', ' WHERE FALSE'), ('literal', '')):
                case = {'id': f'{function}-{kind}-{i}',
                        'sql': f'SELECT {function}({args}){suffix}'}
                if function == 'lcm' and kind == 'literal' and i == 11:
                    case['unsupported'] = {
                        'reason': 'The exact value 1 at retained scale 76 needs 77 coefficient digits, beyond Arrow decimal256 precision 76.',
                        'sqlstate': '22003',
                    }
                cases.append(case)
        for i, pattern in enumerate([
            "SELECT {f}(1::smallint,2::smallint) IS NULL WHERE FALSE",
            "SELECT CAST({f}(1::smallint,2::smallint) AS TEXT) WHERE FALSE",
            "SELECT CASE WHEN FALSE THEN {f}(1::smallint,2::smallint) ELSE 1 END",
            "SELECT 1 WHERE {f}(NULL,NULL) IS NULL",
            "SELECT CAST({f}(NULL::integer,'bad') AS TEXT) WHERE FALSE",
            "SELECT coalesce({f}(NULL::text,1::integer),0) WHERE FALSE",
            "SELECT {f}('-2147483648'::integer,0::integer)",
            "SELECT {f}('-9223372036854775808'::bigint,0::bigint)",
        ]):
            cases.append({'id': f'{function}-wrapped-{i}',
                          'sql': pattern.format(f=function)})
        cases.append({'id': f'{function}-default',
                      'sql': f'CREATE TABLE invalid_default_{function} (i INTEGER DEFAULT CASE WHEN FALSE THEN {function}(NULL,NULL) ELSE 1 END)'})
    return {'area': 'gcd-lcm-overloads', 'setup': [], 'cases': cases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'overloads.json').write_text(json.dumps(corpus(), indent=2) + '\n')


if __name__ == '__main__':
    main()
