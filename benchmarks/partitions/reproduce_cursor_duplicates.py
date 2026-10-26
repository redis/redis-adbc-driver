"""Raw Search cursor duplicate diagnostic on an owner-provided disposable Redis.

No ADBC driver involved. Creates/drops only a fresh generated index/key prefix.
Requires redis-py. Does not change server settings or report benchmark timings.
"""
import argparse
import collections
import json
import os
from pathlib import Path
import platform
import uuid
import time

import redis


def result_ids(page):
    if isinstance(page, dict):
        return [int(r['extra_attributes']['__rowid']) for r in page['results']]
    return [int(dict(zip(r[::2], r[1::2]))['__rowid']) for r in page[1:]]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--wait-index-ready', action='store_true', help='Control: wait for num_docs=10000 and indexing=0 before cursor trials')
    parser.add_argument('--repeats', type=int, default=200)
    args = parser.parse_args()
    if args.output.exists() or args.repeats < 1:
        parser.error('choose a fresh output path and positive repeat count')
    client = redis.Redis.from_url(os.environ['REDIS_URI'], decode_responses=True, protocol=2)
    cap = int(client.config_get('search-max-aggregate-results')['search-max-aggregate-results'])
    if cap < 10000:
        parser.error('diagnostic needs an aggregate cap >=10000; no settings were changed')
    name = 'partition_raw_' + uuid.uuid4().hex[:12]
    prefix = name + ':'
    report = {'platform': platform.platform(), 'redis_version': client.info('server')['redis_version'],
              'search_module': client.execute_command('MODULE', 'LIST'), 'aggregate_cap': cap,
              'rows_ingested': 100000, 'rows_after_delete': 10000,
              'repeats_per_protocol': args.repeats, 'trials': 0, 'failures': []}
    client.execute_command('FT.CREATE', name, 'ON', 'HASH', 'PREFIX', 1, prefix,
                           'SCHEMA', '__rowid', 'NUMERIC', 'SORTABLE',
                           'adv', 'NUMERIC', 'SORTABLE', 'width', 'NUMERIC', 'SORTABLE')
    try:
        for lo in range(1, 100001, 1000):
            pipe = client.pipeline(transaction=False)
            for row in range(lo, lo + 1000):
                pipe.hset(prefix + str(row), mapping={'__rowid': row, 'adv': 2, 'width': 1638})
            pipe.execute()
        for lo in range(1, 100001, 1000):
            client.delete(*(prefix + str(row) for row in range(lo, lo + 1000) if row % 10))
        info = client.execute_command('FT.INFO', name)
        fields = dict(zip(info[::2], info[1::2]))
        report['index_after_delete'] = {k: fields.get(k) for k in ['num_docs', 'indexing', 'percent_indexed', 'gc_stats']}
        if args.wait_index_ready:
            started = time.monotonic()
            while int(fields['num_docs']) != 10000 or int(fields['indexing']) != 0:
                if time.monotonic() - started > 300:
                    raise TimeoutError('Search index did not become ready')
                time.sleep(0.05)
                info = client.execute_command('FT.INFO', name)
                fields = dict(zip(info[::2], info[1::2]))
            report['readiness_wait_seconds'] = time.monotonic() - started
            report['index_before_reads'] = {key: fields.get(key) for key in ['num_docs', 'indexing', 'percent_indexed']}
        expected = set(range(10, 100001, 10))
        for protocol in [2, 3]:
            reader = redis.Redis.from_url(os.environ['REDIS_URI'], decode_responses=True, protocol=protocol)
            try:
                for repeat in range(args.repeats):
                    page, cursor = reader.execute_command('FT.AGGREGATE', name, '@__rowid:[1 100000]',
                        'LOAD', 4, '@__key', '@__rowid', '@width', '@adv',
                        'TIMEOUT', 0, 'WITHCURSOR', 'COUNT', 10000, 'DIALECT', 2)
                    rows = result_ids(page)
                    sizes = [len(rows)]
                    while cursor:
                        page, cursor = reader.execute_command('FT.CURSOR', 'READ', name, cursor, 'COUNT', 10000)
                        more = result_ids(page)
                        rows.extend(more)
                        sizes.append(len(more))
                    report['trials'] += 1
                    if len(rows) != 10000 or set(rows) != expected:
                        failure = {'protocol': protocol, 'repeat': repeat, 'rows': len(rows),
                            'pages': sizes, 'missing_ids': sorted(expected - set(rows)),
                            'unexpected_ids': sorted(set(rows) - expected),
                            'duplicates': [(key, count) for key, count in collections.Counter(rows).items() if count > 1]}
                        report['failures'].append(failure)
                        print(json.dumps(failure), flush=True)
            finally:
                reader.close()
    finally:
        client.execute_command('FT.DROPINDEX', name, 'DD')
        client.close()
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(report['trials'], 'raw trials;', len(report['failures']), 'failures')


if __name__ == '__main__':
    main()
