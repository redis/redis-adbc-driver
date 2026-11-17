"""Build an independent #110 Unicode function matrix outside the curated corpus.

Run: python go/pgdiff/unicode_functions.py /private/output/directory
Compare that directory with pgdiff's --corpus option.
"""
import argparse
import json
from pathlib import Path


def quote(value):
    return "'" + value.replace("'", "''") + "'"


def corpus():
    cases = []
    def add(sql):
        cases.append({'id': f'unicode-{len(cases):04d}', 'sql': sql})
    values = ['', 'ASCII', 'a\u0308', 'Å', '\u212b', '\ufb03', '\u2460',
              '\uac01', '\u1100\u1161\u11a8', '\u0344', '\u0301\u0327',
              'a\u0315\u0300', 'a\u034f\u0300', '😀', 'a' + '\u0315' * 40 + '\u0300',
              '\U00011382\U000113c9']
    for value in values:
        add(f'SELECT normalize({quote(value)})')
        for form in ('NFC', 'NFD', 'NFKC', 'NFKD'):
            add(f'SELECT normalize({quote(value)}, {form})')
            add(f'SELECT pg_catalog.normalize({quote(value)}, {quote(form.lower())})')
    # All Unicode 17 additions with normalization-relevant properties, inert in PG18.
    new = [*range(0x1acf, 0x1ade), *range(0x1ae0, 0x1aec), 0xa7f1,
           0x10efa, 0x10efb, 0x1e6e3, 0x1e6e6, 0x1e6ee, 0x1e6ef, 0x1e6f5]
    for code in new:
        for form in ('NFC', 'NFD', 'NFKC', 'NFKD'):
            add(f"SELECT normalize({quote('a' + chr(code) + chr(0x300))}, {form})")
    escapes = ['', 'plain café😀', r'\0061', r'\+000061', r'\u0061', r'\U00000061',
               r'\uD83D\uDE00', r'\D800\+00DC00', r'\U0000DBFF\uDFFF', r'\\u0061',
               r'\D7FF', r'\E000', r'\+10FFFF', r'\FFFF', r'\0000', r'\u0000',
               r'\+000000', r'\U00000000', r'\+110000', r'\UFFFFFFFF',
               '\\', r'\123', r'\xyz', r'\u123', r'\+12345', r'\U1234567',
               r'\u123x', r'\DC00', r'\D800', r'\D800x', r'\D800\\',
               r'\D800\D800', r'\D800\u0061', r'\D800\xyz', r'\D800\0000',
               r'\00611', r'\U000000611', r'\D800\uDC00\0061']
    for value in escapes:
        for suffix in ('', ' WHERE FALSE'):
            add(f'SELECT unistr({quote(value)}){suffix}')
    for function in ('normalize', 'unistr'):
        for arg in ('NULL', 'NULL::text', '1', 'NULL::integer', 'true',
                    "'x'::bytea", 'ARRAY[1]', "'x'::char(4)", "'x'::varchar(4)"):
            add(f'SELECT {function}({arg})')
        for arg in ('1', 'NULL::integer', "'x'::bytea"):
            add(f'SELECT {function}({arg}) IS NULL WHERE FALSE')
    for query in [
        "SELECT unistr()", "SELECT unistr('a','b')", "SELECT pg_catalog.normalize()",
        "SELECT pg_catalog.normalize('a','NFC','x')", "SELECT normalize()",
        "SELECT normalize('x','NFD')", "SELECT normalize('x',wrong)",
        "SELECT normalize('x',NULL)", "SELECT normalize('x',1)",
        "SELECT normalize('x',NFC,NFD)",
        "SELECT pg_catalog.normalize(NULL,'bad')", "SELECT pg_catalog.normalize('x',NULL)",
        "SELECT pg_catalog.normalize('x',NULL::integer)",
        "SELECT pg_catalog.normalize('x','bad') WHERE FALSE",
        "SELECT pg_catalog.normalize('x','NFC ')",
        'SELECT "normalize"(\'ä\',\'NFD\')',
        "SELECT CASE WHEN FALSE THEN unistr('\\xyz') ELSE 'ok' END",
        "SELECT coalesce('ok',unistr('\\xyz'))",
        "SELECT CASE WHEN FALSE THEN pg_catalog.normalize('x','bad') ELSE 'ok' END",
        "CREATE TABLE invalid_unicode_default (s TEXT DEFAULT unistr(NULL::integer))",
    ]:
        add(query)
    return {'area': 'unicode-functions', 'setup': [], 'cases': cases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'unicode.json').write_text(json.dumps(corpus(), ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
