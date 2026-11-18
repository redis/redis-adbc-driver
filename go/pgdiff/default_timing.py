"""Build independent PostgreSQL18 DEFAULT definition/use comparisons for #479."""
import argparse
import json
from pathlib import Path


def corpus():
    cases = []
    def add(name, steps):
        cases.append({'id': name, 'steps': [{'sql': sql, 'expect_postgres': state} for sql, state in steps]})
    for i, (typ, expr, value, state) in enumerate([
        ('integer', '1/0', '1', '22012'),
        ('text', r"unistr('\xyz')", "'ok'", '42601'),
        ('smallint', '100000', '1', '22003'),
        ('varchar(2)', "'long'", "'ok'", '22001'),
        ('numeric(2,0)', "'1000'", '1', '22003'),
        ('integer', "('bad'::text)::integer", '1', '22P02'),
        ('json', "('bad'::text)::json", "'{}'", '22P02'),
        ('jsonb', "('bad'::text)::jsonb", "'{}'", '22P02'),
    ]):
        table = f'default_use_{i}'
        add(f'deferred-{i}', [
            (f'CREATE TABLE {table}(id integer,b {typ} DEFAULT {expr})', None),
            (f'INSERT INTO {table} VALUES(1,{value}),(2,NULL)', None),
            (f'INSERT INTO {table}(id) VALUES(3)', state),
            (f'SELECT * FROM {table} ORDER BY id', None),
            (f'INSERT INTO {table}(id) SELECT 4 WHERE FALSE', state),
            (f'INSERT INTO {table} SELECT 4,{value} WHERE FALSE', None),
            (f'ALTER TABLE {table} ALTER b SET DEFAULT {expr}', None),
            (f'INSERT INTO {table} VALUES(3,{value})', None),
            (f'ALTER TABLE {table} ALTER b DROP DEFAULT', None),
            (f'INSERT INTO {table}(id) VALUES(4)', None),
            (f'SELECT * FROM {table} ORDER BY id', None),
        ])
    definitions = [
        ("integer DEFAULT 'bad'", '22P02'),
        ("integer DEFAULT '1'::text", '42804'),
        ("integer DEFAULT CAST('bad' AS integer)", '22P02'),
        ("integer DEFAULT CASE WHEN FALSE THEN 'bad'::integer ELSE 1 END", '22P02'),
        ('integer DEFAULT current_timestamp', '42804'),
        ("integer DEFAULT coalesce(NULL,'1')", '42804'),
        ('integer DEFAULT gcd(NULL,NULL)', '42725'),
        ('integer DEFAULT NULL::text', '42804'),
        ('integer DEFAULT 1+true', '42883'),
        ('uuid DEFAULT 1', '42804'), ('uuid DEFAULT true', '42804'),
        ("uuid DEFAULT 'bad'::text", '42804'), ('uuid DEFAULT NULL::text', '42804'),
        ("integer[] DEFAULT ARRAY['x']", '42804'),
        ("integer[] DEFAULT ARRAY['1']", '42804'),
        ('integer[] DEFAULT NULL::text[]', '42804'),
        ('integer[] DEFAULT ARRAY[1::numeric]', None),
        ("integer[] DEFAULT ARRAY[DATE '2024-01-01']::integer[]", '42846'),
        ("integer[] DEFAULT ARRAY['bad']::integer[]", '22P02'),
        ("integer[] DEFAULT ARRAY['1']::integer[]", None),
        ("jsonb DEFAULT '{}'::json", None), ("json DEFAULT '{}'::jsonb", None),
        ('jsonb DEFAULT NULL::json', None),
        ("json DEFAULT 'bad'::json", '22P02'),
        ("jsonb DEFAULT 'bad'::jsonb", '22P02'),
        ("json DEFAULT ('bad'::text)::json", None),
        ('json DEFAULT 1::json', '42846'),
        ("interval DEFAULT INTERVAL 'bad' DAY", '22007'),
        ("interval DEFAULT INTERVAL 'bad' HOUR", '22007'),
        ("interval DEFAULT INTERVAL '1' DAY", None),
        ('date DEFAULT true::date', '42846'),
        ('integer DEFAULT CASE WHEN FALSE THEN NULL::date::integer ELSE 1 END', '42846'),
        ('integer DEFAULT CASE WHEN FALSE THEN 1/0 ELSE 2 END', None),
        ('integer DEFAULT coalesce(2,1/0)', None),
        ('integer DEFAULT NULL', None),
        ('integer DEFAULT nullif(1,1)', None),
        ('integer DEFAULT 1/NULL', None),
    ]
    for i, (definition, state) in enumerate(definitions):
        add(f'definition-{i}', [(f'CREATE TABLE definition_{i}(b {definition})', state)])
    add('alter-type', [
        ('CREATE TABLE default_alter_type(id integer,b integer DEFAULT 1/0)', None),
        ('ALTER TABLE default_alter_type ALTER b TYPE bigint', None),
        ('INSERT INTO default_alter_type VALUES(1,1)', None),
        ('INSERT INTO default_alter_type(id) VALUES(2)', '22012'),
        ('SELECT * FROM default_alter_type', None),
    ])
    add('empty-add', [
        ('CREATE TABLE default_empty_add(id integer)', None),
        ('ALTER TABLE default_empty_add ADD b integer DEFAULT 1/0', '22012'),
        ('INSERT INTO default_empty_add VALUES(1)', None),
        ('SELECT * FROM default_empty_add', None),
    ])
    add('populated-add', [
        ('CREATE TABLE default_pop_add(id integer)', None),
        ('INSERT INTO default_pop_add VALUES(1),(2)', None),
        ('ALTER TABLE default_pop_add ADD b integer DEFAULT 1/0', '22012'),
        ('SELECT * FROM default_pop_add ORDER BY id', None),
        ('ALTER TABLE default_pop_add ADD b integer DEFAULT 2', None),
        ('SELECT * FROM default_pop_add ORDER BY id', None),
    ])
    add('merge', [
        ('CREATE TABLE default_merge(id integer,b integer DEFAULT 1/0)', None),
        ('INSERT INTO default_merge VALUES(1,1)', None),
        ('MERGE INTO default_merge t USING (SELECT 2 AS id) s ON t.id=s.id WHEN NOT MATCHED THEN INSERT(id) VALUES(s.id)', '22012'),
        ('SELECT * FROM default_merge', None),
    ])
    add('volatile-side-effects', [
        ("SELECT set_config('application_name','base',false)", None),
        ("CREATE TABLE default_effect(id integer,s text DEFAULT set_config('application_name',current_setting('application_name')||'x',false))", None),
        ("SELECT current_setting('application_name')", None),
        ("INSERT INTO default_effect VALUES(0,'explicit')", None),
        ("INSERT INTO default_effect(id) SELECT 1 WHERE FALSE", None),
        ("SELECT current_setting('application_name')", None),
        ("INSERT INTO default_effect(id) VALUES(1),(2),(3)", None),
        ("SELECT * FROM default_effect ORDER BY id", None),
    ])
    add('sequence-side-effects', [
        ("CREATE SEQUENCE default_seq START WITH 10", None),
        ("CREATE TABLE default_sequence(id bigint DEFAULT CASE WHEN TRUE THEN nextval('default_seq') ELSE 0 END,b integer DEFAULT 1/0)", None),
        ("INSERT INTO default_sequence DEFAULT VALUES", '22012'),
        ("SELECT nextval('default_seq')", None),
        ("ALTER TABLE default_sequence ALTER b SET DEFAULT 1", None),
        ("INSERT INTO default_sequence DEFAULT VALUES", None),
        ("SELECT * FROM default_sequence", None),
        ("INSERT INTO default_sequence(b) SELECT 1 WHERE FALSE", None),
        ("INSERT INTO default_sequence(b) VALUES(1),(2)", None),
        ("SELECT * FROM default_sequence ORDER BY id", None),
        ("ALTER TABLE default_sequence ALTER id SET DEFAULT CASE WHEN FALSE THEN 0 ELSE nextval('default_seq') END", None),
        ("INSERT INTO default_sequence(b) SELECT 1 WHERE FALSE", None),
        ("SELECT nextval('default_seq')", None),
    ])
    add('typed-null-metadata', [
        ('CREATE TABLE default_null_meta(s text DEFAULT (NULL::integer)::text)', None),
        ("SELECT column_default IS NOT NULL FROM information_schema.columns WHERE table_name='default_null_meta' AND column_name='s'", None),
        ('ALTER TABLE default_null_meta ALTER s TYPE integer', '42804'),
        ('INSERT INTO default_null_meta DEFAULT VALUES', None),
        ('SELECT * FROM default_null_meta', None),
    ])
    return {'area': 'default-timing', 'setup': [], 'cases': cases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / 'defaults.json').write_text(json.dumps(corpus(), indent=2) + '\n')


if __name__ == '__main__':
    main()
