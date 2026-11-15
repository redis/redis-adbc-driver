#!/usr/bin/env python3
"""Adversarial exact-source contract on an owned, verified Software fixture.

No new server feature is used. This validates the existing driver/raw controls,
shows what ordinary aliased LOAD returns, and quantifies LOAD* overfetch. It is
not a SQL performance comparison or evidence that the prototype is installed.
"""
import argparse
from decimal import Decimal, InvalidOperation
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import subprocess
import time
import uuid
from urllib.parse import parse_qs, urlsplit

import adbc_driver_manager.dbapi as adbc
import pyarrow as pa
import redis
from redis.backoff import NoBackoff
from redis.retry import Retry


SCHEMA = pa.schema([('id', pa.int64()), ('n', pa.int64()), ('label', pa.string())])
NUMBERS = [-(1 << 63), (1 << 63) - 1, 1 << 53, (1 << 53) + 1,
           -(1 << 53) - 1, 0, None, 1]


def attributes(reply):
    assert isinstance(reply, dict) and not reply.get(b'warning') and not reply.get(b'warnings')
    return [r[b'extra_attributes'] for r in reply[b'results']]


def aggregate(r, index, query, load):
    """Fully fetch every RESP3 cursor page and independently close a live cursor."""
    cursor = 0
    out = []
    try:
        reply = r.execute_command('FT.AGGREGATE', index, query, *load,
                                  'TIMEOUT', 0, 'WITHCURSOR', 'COUNT', 3, 'DIALECT', 2)
        while True:
            page, cursor = reply
            out.extend(attributes(page))
            if not cursor:
                break
            reply = r.execute_command('FT.CURSOR', 'READ', index, cursor, 'COUNT', 3)
    finally:
        if cursor:
            r.execute_command('FT.CURSOR', 'DEL', index, cursor)
    return out


def ready(r, index, count):
    end = time.monotonic() + 30
    while time.monotonic() < end:
        info = r.execute_command('FT.INFO', index)
        if not isinstance(info, dict):
            info = dict(zip(info[::2], info[1::2]))
        if int(info[b'num_docs']) == count and int(info.get(b'indexing', 0)) == 0:
            return
        time.sleep(.02)
    raise AssertionError('source index did not become ready')


def typed(q, sql, expected):
    q.execute(sql)
    actual = q.fetch_arrow_table().combine_chunks()
    assert actual.column_names == expected.column_names
    assert all(a.type == b.type for a, b in zip(actual.schema, expected.schema))
    # Preserve arrays/types/NULL values while normalizing field metadata and
    # nullable declarations, as in the established table-mode oracle.
    actual = pa.Table.from_arrays(actual.columns, schema=expected.schema)
    assert actual.equals(expected, check_metadata=False), 'complete typed SQL result differs'


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--driver', type=Path, required=True)
    p.add_argument('--cli', type=Path, required=True)
    p.add_argument('--source-commit', required=True)
    p.add_argument('--gate', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--dedicated-empty-server', action='store_true', required=True)
    a = p.parse_args()
    gate = json.loads(a.gate.read_text())
    assert gate['qpf'] == 4 and gate['workers'] == 6 and gate['routing'] == 'mnp'
    assert gate['connections'] == 32 and gate['tls_certificate_verified'] and gate['resp'] == 3
    assert 0 <= time.time() - a.gate.stat().st_mtime < 60, 'run the coordinator gate immediately before invocation'
    uri, ca = os.environ['REDIS_URI'], os.environ['SSL_CERT_FILE']
    assert uri.startswith('rediss://')
    assert parse_qs(urlsplit(uri).query).get('protocol', ['3']) == ['3']
    token = uuid.uuid4().hex
    prefix = 'projection_source_' + token + ':'
    index, table = 'projection_source_' + token, 'projection_source_' + token
    marker, owner = 'projection:repro:owner', token.encode()
    report = {'scope': 'Exact-source API contract/repro; no SQL timing, server CPU, or installed prototype capability claim',
              'driver_source_commit': a.source_commit,
              'driver_sha256': hashlib.sha256(a.driver.read_bytes()).hexdigest(),
              'cli_sha256': hashlib.sha256(a.cli.read_bytes()).hexdigest(),
              'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              'prototype_installed': False,
              'prototype_wire_command': 'not implemented by the library prototype',
              'qpf': 4, 'resp': 3, 'strict_tls': True, 'caller_retries': 0,
              'packages': {name: importlib.metadata.version(name) for name in ('pyarrow', 'adbc-driver-manager', 'redis')},
              'validated': False, 'queries': [], 'cleanup': False}
    r = redis.Redis.from_url(uri, ssl_ca_certs=ca, ssl_cert_reqs='required',
                            ssl_check_hostname=True, socket_timeout=30,
                            retry=Retry(NoBackoff(), 0), retry_on_error=[], protocol=3)
    started = False
    try:
        assert r.dbsize() == 0 and not r.execute_command('FT._LIST')
        assert int(r.client_info()['resp']) == 3
        assert r.set(marker, owner, nx=True)
        started = True
        expected = {}
        for i, n in enumerate(NUMBERS, 1):
            label = None if i == 7 else '' if i == 6 else 'a' + chr(0) + 'b' if i == 5 else f'row-{i}'
            values = {b'id': str(i).encode()}
            if n is not None:
                values[b'n'] = str(n).encode()
            if label is not None:
                values[b'label'] = label.encode()
            r.hset(prefix + str(i), mapping=values)
            expected[i] = {'id': i, 'n': n, 'label': label}
        assert 0 in expected[5]['label'].encode()
        r.execute_command('FT.CREATE', index, 'ON', 'HASH', 'PREFIX', 1, prefix,
                          'SCHEMA', 'id', 'NUMERIC', 'SORTABLE', 'n', 'NUMERIC', 'SORTABLE')
        ready(r, index, len(expected))
        command = [str(a.cli), 'adopt', '-prefix', prefix, '-table', table,
                   '-index', index, '-mode', 'unmanaged', '-apply', '-json']
        for name, sql_type in [('id', 'BIGINT'), ('n', 'BIGINT'), ('label', 'TEXT')]:
            command += ['-type', name + '=' + sql_type, '-encoding', name + '=native']
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90)
        assert result.returncode == 0, 'adoption failed; details intentionally kept out of public evidence'

        # Add unregistered fields after registration: a loader cannot infer their
        # size from the public SQL column count. All requested fields stay small.
        for i in expected:
            r.hset(prefix + str(i), mapping={b'unselected_wide': b'x' * (16 << 10),
                                            b'binary' + bytes([0]) + b'field': b'a' + bytes([0, 255]) + b'b'})
        ready(r, index, len(expected))
        key_ids = {(prefix + str(i)).encode(): i for i in expected}
        raw = {i: r.hgetall(key) for key, i in key_ids.items()}
        assert all(v[b'id'] == str(i).encode() and v.get(b'n') ==
                   (None if expected[i]['n'] is None else str(expected[i]['n']).encode()) for i, v in raw.items())
        assert raw[5][b'label'] == b'a' + bytes([0]) + b'b'
        assert all(v[b'binary' + bytes([0]) + b'field'] == b'a' + bytes([0, 255]) + b'b' for v in raw.values())
        assert r.hmget(prefix + '7', ['n', 'label']) == [None, None]
        assert r.hmget(prefix + 'missing', ['n', 'label']) == [None, None]
        assert r.type(prefix + '7') == b'hash' and r.type(prefix + 'missing') == b'none'
        report['all_null_projection_presence_ambiguity'] = {
            'existing_hash_and_missing_key_have_same_hmget_vector': True,
            'native_types_distinguish_them': True,
            'interpretation': 'Separate TYPE observations are controls, not an atomic projection/presence API'}

        query = '@id:[1 8]'
        alias = aggregate(r, index, query, ['LOAD', 1, '@__key', 'LOAD', 3, '@n', 'AS', 'raw_n'])
        assert len(alias) == len(expected) and {x[b'__key'] for x in alias} == set(key_ids)
        mismatches = []
        numeric_mismatches = []
        for row in alias:
            i = key_ids[row[b'__key']]
            want = raw[i].get(b'n')
            got = row.get(b'raw_n')
            if got != want:
                mismatches.append(i)
            if want is not None:
                try:
                    text = got.decode() if isinstance(got, bytes) else str(got)
                    exact = Decimal(text) == Decimal(want.decode())
                except (InvalidOperation, ValueError):
                    exact = False
                if not exact:
                    numeric_mismatches.append(i)
        report['ordinary_aliased_load'] = {'rows': len(alias), 'raw_byte_mismatch_case_ids': sorted(mismatches),
                                         'numeric_value_mismatch_case_ids': sorted(numeric_mismatches),
                                         'interpretation': 'Observed native behavior, not an expected exact-source API'}

        all_rows = aggregate(r, index, query, ['LOAD', 1, '@__key', 'LOAD', '*'])
        assert len(all_rows) == len(expected)
        loaded_bytes = 0
        projected_bytes = 0
        for row in all_rows:
            i = key_ids[row[b'__key']]
            for field in (b'id', b'n', b'label'):
                assert row.get(field) == raw[i].get(field)
                projected_bytes += len(raw[i].get(field, b''))
            assert row[b'unselected_wide'] == b'x' * (16 << 10)
            loaded_bytes += sum(len(v) for k, v in row.items() if k != b'__key' and isinstance(v, bytes))
        assert loaded_bytes >= (16 << 10) * len(expected)
        report['load_all'] = {'rows': len(all_rows), 'selected_public_value_bytes': projected_bytes,
                              'returned_value_bytes_excluding_document_keys': loaded_bytes,
                              'unselected_wide_bytes_per_row': 16 << 10,
                              'interpretation': 'Raw projection preserves selected values but enumerates/returns unselected data'}

        with adbc.connect(driver=str(a.driver), autocommit=True, db_kwargs={'uri': uri}) as c, c.cursor() as q:
            def oracle(name):
                values = [expected[i] for i in sorted(expected)]
                typed(q, f'SELECT id,n,label FROM {table} WHERE id >= 1 ORDER BY id',
                      pa.Table.from_pylist(values, schema=SCHEMA))
                numbers = [x['n'] for x in values if x['n'] is not None]
                typ = pa.schema([('row_count', pa.int64()), ('nonnull', pa.int64()), ('total', pa.int64())])
                typed(q, f'SELECT CAST(COUNT(*) AS BIGINT) AS row_count,CAST(COUNT(n) AS BIGINT) AS nonnull,'
                      f'CAST(SUM(n) AS BIGINT) AS total FROM {table} WHERE id >= 1',
                      pa.Table.from_pylist([{'row_count': len(values), 'nonnull': len(numbers), 'total': sum(numbers)}], schema=typ))
                report['queries'].append({'case': name, 'typed_projection_rows': len(values), 'typed_aggregate_rows': 1,
                                          'validated': True})
            oracle('int64_extrema_adjacent_null_empty_actual_nul')
            nullable = pa.schema([('n', pa.int64()), ('label', pa.string())])
            typed(q, f"SELECT n,label FROM {table} WHERE __key='{prefix}7'",
                  pa.Table.from_pylist([{'n': None, 'label': None}], schema=nullable))
            report['queries'].append({'case': 'existing_hash_all_selected_null',
                                      'typed_projection_rows': 1, 'validated': True})
            r.hset(prefix + '8', 'n', str((1 << 53) + 3))
            expected[8]['n'] = (1 << 53) + 3
            # Keep the SUM in signed int64 range while exposing exact live changes.
            oracle('source_change_between_statements')
            r.delete(prefix + '3', prefix + '4')
            r.set(prefix + '4', b'wrongtype')
            del expected[3], expected[4]
            ready(r, index, len(expected))
            oracle('deleted_and_non_hash_sources')
            q.execute(f'DROP TABLE {table}')
        assert r.get(prefix + '4') == b'wrongtype'
        assert r.hget(prefix + '8', 'n') == str((1 << 53) + 3).encode()
        assert r.hget(prefix + '5', b'binary' + bytes([0]) + b'field') == b'a' + bytes([0, 255]) + b'b'
        assert index.encode() in r.execute_command('FT._LIST'), 'DROP removed the application index'
        report['source_ownership_preserved_after_drop'] = True
        report['validated'] = True
    finally:
        try:
            if started and r.get(marker) == owner:
                # The flag, initially empty database, exclusive coordinator slot and
                # owner marker jointly authorize clearing only this disposable run.
                indexes = r.execute_command('FT._LIST')
                assert set(indexes) <= {index.encode()}, 'unexpected index; preserve resources for inspection'
                if index.encode() in indexes:
                    r.execute_command('FT.DROPINDEX', index)
                r.flushdb()
                report['cleanup'] = r.dbsize() == 0 and not r.execute_command('FT._LIST')
        finally:
            try:
                r.close()
            finally:
                a.output.parent.mkdir(parents=True, exist_ok=True)
                a.output.write_text(json.dumps(report, indent=2) + '\n')
    assert report['validated'] and report['cleanup']
    print('Exact driver/raw oracles and cleanup passed; no installed prototype or performance claim.')


if __name__ == '__main__':
    main()
