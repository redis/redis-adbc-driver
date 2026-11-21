"""PostgreSQL 18 DEFAULT grammar/type identity comparisons for #481/#484."""
import argparse
import json
from pathlib import Path


def corpus():
    cases = []
    def add(name, steps):
        cases.append({"id": name, "steps": [{"sql": sql, "expect_postgres": state} for sql, state in steps]})
    for i, expr in enumerate([
        "1 IS NULL", "1 IS NOT NULL", "true IS TRUE", "true IS NOT UNKNOWN",
        "1 ISNULL", "1 NOTNULL", "NULL::integer IS NORMALIZED", "true AND false",
        "true OR false", "NOT true", "1 IN (1)", "1 BETWEEN 0 AND 2",
        "'a' LIKE 'a'", "'a' NOT ILIKE 'b'", "1 = ANY(ARRAY[1])",
        "TIMESTAMP '2024-01-01' AT TIME ZONE 'UTC'",
    ]):
        table = f"grammar_refused_{i}"
        add(f"grammar-refused-{i}", [
            (f"CREATE TABLE {table}(v boolean DEFAULT {expr})", "42601"),
            (f"SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='{table}'", None),
            (f"CREATE TABLE {table}(id integer)", None),
            (f"ALTER TABLE {table} ADD v boolean DEFAULT {expr}", "42601"),
            (f"SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='{table}'", None),
        ])
    for i, (typ, expr) in enumerate([
        ("boolean", "(1 IS NULL)"), ("boolean", "(true AND false)"),
        ("boolean", "(NOT true)"), ("boolean", "(1 IN (1))"),
        ("boolean", "1 IS DISTINCT FROM 2"), ("boolean", "1 IS NOT DISTINCT FROM 2"),
        ("boolean", "1<2"), ("integer", "1+2*3"),
        ("boolean", "coalesce((1 IS NULL),false)"),
        ("boolean", "CASE WHEN 1 IS NULL THEN true ELSE false END"),
        ("boolean", "('a' LIKE 'a')"),
    ]):
        table = f"grammar_valid_{i}"
        add(f"grammar-valid-{i}", [
            (f"CREATE TABLE {table}(v {typ} DEFAULT {expr} NOT NULL)", None),
            (f"INSERT INTO {table} DEFAULT VALUES", None),
            (f"SELECT * FROM {table}", None),
            (f"ALTER TABLE {table} ALTER v SET DEFAULT {expr}", None),
            (f"INSERT INTO {table} DEFAULT VALUES", None),
            (f"SELECT * FROM {table}", None),
        ])
    add("default-column-collation", [
        ('CREATE TABLE grammar_collate(v text DEFAULT \'a\' COLLATE "C" NOT NULL)', None),
        ('INSERT INTO grammar_collate DEFAULT VALUES', None),
        ('SELECT * FROM grammar_collate', None),
    ])
    add("set-default-a-expr", [
        ("CREATE TABLE grammar_set(v boolean)", None),
        ("ALTER TABLE grammar_set ALTER v SET DEFAULT 1 IS NULL", None),
        ("INSERT INTO grammar_set DEFAULT VALUES", None),
        ("ALTER TABLE grammar_set ALTER v SET DEFAULT true AND false", None),
        ("INSERT INTO grammar_set DEFAULT VALUES", None),
        ("SELECT * FROM grammar_set", None),
        ("ALTER TABLE grammar_set ALTER v SET DEFAULT NULL::integer IS NORMALIZED", "42883"),
        ("CREATE TABLE grammar_signature(v boolean DEFAULT (NULL::integer IS NORMALIZED))", "42883"),
    ])
    for i, (old, new, expr) in enumerate([
        ("numeric(4,2)", "integer", "'1.50'"),
        ("numeric(4,2)", "integer", "'-1.50'"),
        ("numeric(4,2)", "integer", "'1.499'"),
        ("numeric(4,2)", "numeric(6,3)", "'1.555'"),
        ("numeric(2,0)", "numeric(6,2)", "'1000'"),
        ("integer", "numeric(6,3)", "1.5"),
        ("varchar(2)", "text", "'long'"), ("char(2)", "text", "'long'"),
        ("boolean", "text", "'true'"),
        ("uuid", "text", "'A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11'"),
        ("date", "text", "'2024-01-02'"),
        ("timestamptz", "text", "'2024-01-02 03:04:05+00'"),
        ("integer[]", "bigint[]", "'{1,2}'"),
    ]):
        table = f"retained_type_{i}"
        add(f"retained-type-{i}", [
            (f"CREATE TABLE {table}(id integer,v {old} DEFAULT {expr})", None),
            (f"ALTER TABLE {table} ALTER v TYPE {new}", None),
            (f"INSERT INTO {table}(id) VALUES(1),(2)", None),
            (f"INSERT INTO {table} VALUES(3,NULL)", None),
            (f"SELECT * FROM {table} ORDER BY id", None),
        ])
    add("retained-invalid-atomic", [
        ("CREATE TABLE retained_atomic(id integer,v text DEFAULT '1')", None),
        ("INSERT INTO retained_atomic VALUES(1,'3')", None),
        ("ALTER TABLE retained_atomic ALTER v TYPE integer USING v::integer", "42804"),
        ("INSERT INTO retained_atomic(id) VALUES(2)", None),
        ("SELECT * FROM retained_atomic ORDER BY id", None),
    ])
    add("retained-deferred", [
        ("CREATE TABLE retained_deferred(id integer,v integer DEFAULT ('bad'::text)::integer)", None),
        ("ALTER TABLE retained_deferred ALTER v TYPE bigint", None),
        ("INSERT INTO retained_deferred VALUES(1,2),(2,NULL)", None),
        ("INSERT INTO retained_deferred(id) VALUES(3)", "22P02"),
        ("SELECT * FROM retained_deferred ORDER BY id", None),
    ])
    add("retained-timezone-identity", [
        ("SET TIME ZONE 'America/New_York'", None),
        ("CREATE TABLE retained_zone(v timestamptz DEFAULT '2024-01-02 03:04:05')", None),
        ("SET TIME ZONE 'UTC'", None),
        ("ALTER TABLE retained_zone ALTER v TYPE text", None),
        ("INSERT INTO retained_zone DEFAULT VALUES", None),
        ("SELECT * FROM retained_zone", None),
    ])
    return {"area": "default-repairs", "setup": [], "cases": cases}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    args.directory.mkdir(parents=True, exist_ok=True)
    (args.directory / "defaults.json").write_text(json.dumps(corpus(), indent=2) + "\n")
