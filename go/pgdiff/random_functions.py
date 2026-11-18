"""Independent stateful PostgreSQL18 random-function cases outside curated SQL.

Normal results are explicitly rounded in the SQL to nine fractional digits:
Go math and PostgreSQL's platform C library can differ by a few ULP. Uniform
outputs remain exact binary comparisons and no comparison policy is changed.
"""
import argparse
import json
from pathlib import Path


def corpus():
    cases = []
    def add(sql, **kwargs):
        cases.append(dict(id=f'random-{len(cases):03d}', sql=sql, **kwargs))
    for seed in ('-1', '-0.5', '0', '0.1', '0.5', '1'):
        add(f'SELECT setseed({seed})::text')
        add('SELECT random(),random(),random()')
        for args in ('', '2', '2,3', '2,-3', '2,0', 'stddev=>0', 'stddev:=3,mean:=2'):
            add(f'SELECT round(random_normal({args})::numeric,9)')
            add('SELECT random()')
        add(f'SELECT setseed({seed})::text')
        add('SELECT random(),random()')
    for function in ('setseed', 'random_normal'):
        for arg in ('0::smallint', '0::integer', '0::bigint', '0::numeric(6,2)',
                    '0::real', '0::float8', 'NULL', 'NULL::integer', 'NULL::float8',
                    "'0.5'", "'bad'", "'0.5'::text", 'NULL::text', 'true', "'a'::bytea"):
            for suffix in ('', ' WHERE FALSE'):
                if function == 'setseed':
                    add(f'SELECT setseed({arg})::text{suffix}')
                else:
                    add(f'SELECT round(random_normal({arg})::numeric,9){suffix}')
        for bad in ("'NaN'", "'Infinity'", "'-Infinity'", '2', '-2'):
            if function == 'setseed':
                add(f'SELECT setseed({bad})::text')
                add(f'SELECT setseed({bad})::text WHERE FALSE')
    for query in [
        'SELECT setseed()','SELECT setseed(0,1)', 'SELECT random_normal(1,2,3)',
        'SELECT setseed(seed=>0.5)', 'SELECT random_normal(wrong=>0)',
        'SELECT random_normal(mean=>1,2)', 'SELECT random_normal(mean=>1,mean=>2)',
        'SELECT random_normal(1,mean=>2)',
        'SELECT pg_typeof(setseed(.5)),setseed(.5) IS NULL,setseed(NULL) IS NULL',
        "SELECT random_normal('NaN'),random_normal('Infinity',0)",
        'SELECT setseed(NULL)::text', 'SELECT setseed(.5)::text',
        'SELECT CASE WHEN FALSE THEN setseed(-.5) ELSE NULL END::text',
        'SELECT random()', 'SELECT random_normal() WHERE FALSE', 'SELECT random()',
        "SELECT setseed(.5) = ''::text", 'SELECT setseed(.5)=setseed(.5)',
        'SELECT setseed(.5)::integer', 'SELECT setseed(.5)::boolean',
        "SELECT setseed(.5)||'a'", "SELECT 'a'||setseed(.5)",
        "SELECT v='' FROM (SELECT setseed(.5) AS v) s", "SELECT upper(v) IS NULL FROM (SELECT setseed(.5) AS v) s",
        'SELECT upper(setseed(.5))', "SELECT concat(setseed(.5),'a')",
        "SELECT pg_typeof(coalesce(setseed(NULL),'x')),coalesce(setseed(NULL),'x')::text",
        'CREATE TABLE random_void_table AS SELECT setseed(.5) AS v',
        'CREATE VIEW random_void_view AS SELECT setseed(.5) AS v',
    ]:
        add(query)
    for connection in ('seed_a', 'seed_b'):
        add('SELECT setseed(.5)::text', connection=connection)
        add('SELECT random()', connection=connection)
    add('SELECT setseed(-.5)::text', connection='seed_a')
    add('SELECT random()', connection='seed_b')
    # Retain the pre-existing selected-volatile CASE planner regression (#479).
    # Without the shared purity fix, its extra planning draw is a visible failure.
    add('SELECT setseed(.5)::text')
    add('SELECT CASE WHEN TRUE THEN random() ELSE 0 END')
    add('SELECT random()')
    return dict(area='random-functions', setup=[], cases=cases)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'random.json').write_text(json.dumps(corpus(), indent=2) + '\n')


if __name__ == '__main__':
    main()
