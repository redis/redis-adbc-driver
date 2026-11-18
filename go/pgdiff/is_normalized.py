"""Build an independent PostgreSQL18 IS NORMALIZED comparison corpus.

Run: python go/pgdiff/is_normalized.py /private/corpus
Then use pgdiff --corpus on that directory. Curated SQL stays unchanged.
"""
import argparse
import json
from pathlib import Path


def corpus():
    cases = []
    def add(sql):
        cases.append({'id': f'normalized-{len(cases):04d}', 'sql': sql})
    def quote(value):
        return "'" + value.replace("'", "''") + "'"
    values = ['', 'ASCII', 'a\u0308', 'ä', '\u212b', '\ufb03', '\u2460',
              '\uac01', '\u1100\u1161\u11a8', '\u0344', '\u0301\u0327',
              'a\u0315\u0300', 'a\u034f\u0300', '😀', 'a' + '\u0315' * 40,
              'a' + '\u0315' * 40 + '\u0300', '\U00011383']
    for value in values:
        q = quote(value)
        for form in ('', 'NFC ', 'NFD ', 'NFKC ', 'NFKD '):
            for negation in ('', 'NOT '):
                add(f'SELECT {q} IS {negation}{form}NORMALIZED')
        add(f'SELECT is_normalized({q})')
        for form in ('NFC', 'NFD', 'NFKC', 'NFKD'):
            add(f'SELECT is_normalized({q},{quote(form.lower())})')
    new = [*range(0x1acf, 0x1ade), *range(0x1ae0, 0x1aec), 0xa7f1,
           0x10efa, 0x10efb, 0x1e6e3, 0x1e6e6, 0x1e6ee, 0x1e6ef, 0x1e6f5]
    for code in new:
        for form in ('NFC', 'NFD', 'NFKC', 'NFKD'):
            add(f"SELECT {quote('a' + chr(code) + chr(0x300))} IS {form} NORMALIZED")
    for expr in ('NULL', 'NULL::text', 'NULL::integer', '1', 'true',
                 "'x'::char(5)", "'x'::varchar(5)", "'x'::bytea", 'ARRAY[1]'):
        for suffix in ('', ' WHERE FALSE'):
            add(f'SELECT {expr} IS NORMALIZED{suffix}')
            add(f'SELECT {expr} IS NOT NFD NORMALIZED{suffix}')
    for sql in [
        "SELECT pg_catalog.is_normalized('ä','nfc')",
        'SELECT "is_normalized"(\'ä\',\'nfd\')',
        "SELECT is_normalized(NULL,'bad')", "SELECT is_normalized('a',NULL)",
        "SELECT is_normalized('a',NULL::integer)", "SELECT is_normalized('a','bad') WHERE FALSE",
        "SELECT is_normalized()", "SELECT is_normalized('x','NFC','extra')",
        "SELECT 'x' IS 'NFC' NORMALIZED", "SELECT 'x' IS WRONG NORMALIZED",
        "SELECT 'x' IS NFC", "SELECT 'x' IS NOT NORMALIZED NFC NFD",
        "SELECT ('ä' IS NORMALIZED) IS TRUE", "SELECT 'ä' IS NORMALIZED = TRUE",
        "SELECT 'a' || '̈' IS NOT NORMALIZED AND TRUE",
        "SELECT 1 WHERE 'ä' IS NORMALIZED OR 'ä' IS NORMALIZED",
        "SELECT ('ä' IS NOT NFKD NORMALIZED) IS NULL",
        "SELECT CASE WHEN FALSE THEN is_normalized('x','bad') ELSE TRUE END",
        "SELECT CASE WHEN FALSE THEN 1 IS NORMALIZED ELSE TRUE END",
        "SELECT coalesce(TRUE,is_normalized('x','bad'))",
        "CREATE TABLE bad_normalized_default (b BOOLEAN DEFAULT (NULL::integer IS NORMALIZED))",
        "CREATE TABLE lazy_normalized_default (b BOOLEAN DEFAULT CASE WHEN FALSE THEN is_normalized('x','bad') ELSE TRUE END)",
    ]:
        add(sql)
    return {'area': 'is-normalized', 'setup': [], 'cases': cases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'is-normalized.json').write_text(json.dumps(corpus(), ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
