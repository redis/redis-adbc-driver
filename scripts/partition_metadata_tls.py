"""Native metadata acceptance using the owned partition TLS fixture's principals.

Principals are provisioned through the selected fixture backend and cleaned up by
its existing lifecycle. Never log URIs, passwords, or metadata payloads.
"""
import secrets
from urllib.parse import quote, urlsplit, urlunsplit

import adbc_driver_manager.dbapi as dbapi


def check_metadata_tls(principals, connect, planner_uri, schema, read_commands):
    table = 'metadata_rows'
    qualified = schema + '.' + table
    checks = []

    def statistics(conn, approximate):
        with conn.adbc_get_statistics(db_schema_filter=schema,
                                      table_name_filter=table,
                                      approximate=approximate) as reader:
            rows = reader.read_all().to_pylist()
        return [stat for catalog in rows for namespace in catalog['catalog_db_schemas']
                for stat in namespace['db_schema_statistics']]

    def expect_count(conn, approximate):
        assert statistics(conn, approximate) == [{
            'table_name': table, 'column_name': None, 'statistic_key': 6,
            'statistic_value': 3, 'statistic_is_approximate': True,
        }]

    def expect_denied(conn, approximate):
        try:
            statistics(conn, approximate)
        except dbapi.Error as error:
            assert 'NOPERM' in str(error), 'statistics failed for an unexpected reason'
        else:
            raise AssertionError('denied statistics returned a result instead of failing')

    def constraints(conn):
        with conn.adbc_get_objects(depth='all', db_schema_filter=schema,
                                   table_name_filter=table) as reader:
            objects = reader.read_all().to_pylist()
        tables = [t for catalog in objects for namespace in catalog['catalog_db_schemas']
                  for t in namespace['db_schema_tables']]
        assert len(tables) == 1
        declared = tables[0]['table_constraints']
        by_name = {constraint['constraint_name']: constraint for constraint in declared}
        assert len(by_name) == len(declared), 'duplicate constraint metadata'
        return by_name

    def expect_constraints(conn, key_columns, check_columns, has_check=True):
        expected = {'metadata_pk': {'constraint_name': 'metadata_pk',
            'constraint_type': 'PRIMARY KEY', 'constraint_column_names': key_columns,
            'constraint_column_usage': None}}
        if has_check:
            expected['metadata_check'] = {'constraint_name': 'metadata_check',
                'constraint_type': 'CHECK', 'constraint_column_names': check_columns,
                'constraint_column_usage': None}
        assert constraints(conn) == expected, 'constraint names, columns or usage mismatch'

    # Exercise declared key order, CHECK reference deduplication, and ignored
    # UNIQUE/FOREIGN KEY declarations without claiming enforcement.
    with connect(planner_uri) as planner, planner.cursor() as cur:
        cur.execute(f'CREATE TABLE {qualified} (a BIGINT, b BIGINT, label TEXT, '
                    'CONSTRAINT metadata_pk PRIMARY KEY (b,a), '
                    'CONSTRAINT metadata_check CHECK (a + b > 0 AND a > 0), '
                    'UNIQUE (label), FOREIGN KEY (a) REFERENCES ' + qualified + ' (a))')
        cur.execute(f"INSERT INTO {qualified} VALUES (1,2,'x'),(1,2,'x'),(3,4,NULL)")

        # Independent identities isolate Search, SCAN and source-key permissions.
        for role in ('both', 'hash', 'catalog', 'noscan'):
            # Email principals work with Software's management API and are also
            # valid protocol ACL usernames on the other fixture backends.
            name = schema + '_metadata_' + role + '@metadata.invalid'
            password = 'Aa1!' + secrets.token_urlsafe(21)
            commands = [command for command in read_commands
                        if not (role == 'hash' and command == '+ft.search')]
            keys = ['%R~adbc:*', '%R~idx:' + schema + ':*']
            if role != 'catalog':
                keys.append('%R~' + schema + ':*')
            principals.create(name, password, [*commands, *(['+scan'] if role != 'noscan' else []), *keys])
            endpoint = urlsplit(planner_uri)
            uri = urlunsplit((endpoint.scheme, quote(name) + ':' + quote(password)
                + '@' + endpoint.netloc.split('@')[-1], endpoint.path, '', ''))
            with connect(uri) as worker:
                with worker.adbc_get_statistic_names() as reader:
                    assert reader.read_all().num_rows == 0
                expect_constraints(worker, ['b', 'a'], ['a', 'b'])
                if role == 'both':
                    expect_count(worker, True)
                    expect_count(worker, False)
                    # Metadata must follow DDL, including quoted identifiers,
                    # while preserving primary-key declaration order.
                    cur.execute(f'ALTER TABLE {qualified} RENAME COLUMN a TO "Renamed a"')
                    expect_constraints(worker, ['b', 'Renamed a'], ['Renamed a', 'b'])
                    cur.execute(f'ALTER TABLE {qualified} DROP CONSTRAINT metadata_check')
                    expect_constraints(worker, ['b', 'Renamed a'], [], has_check=False)
                    cur.execute(f'ALTER TABLE {qualified} RENAME COLUMN "Renamed a" TO a')
                    cur.execute(f'ALTER TABLE {qualified} ADD CONSTRAINT metadata_check CHECK (a + b > 0 AND a > 0)')
                    checks.append('restricted_statistics_both_paths_and_constraint_ddl')
                elif role == 'hash':
                    expect_count(worker, False)
                    expect_denied(worker, True)
                    checks.append('hash_statistics_without_search_permission')
                elif role == 'catalog':
                    expect_denied(worker, False)
                    checks.append('hash_statistics_key_acl_denial_is_not_zero')
                else:
                    expect_count(worker, True)
                    expect_denied(worker, False)
                    checks.append('search_statistics_without_scan_permission')
        cur.execute('DROP TABLE ' + qualified)
    return {'status': 'passed', 'checks': checks}
