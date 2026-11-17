"""Source-bound comparison contracts, separate from SQL fixtures and allowlists.

These seven policies resolve #364. They cannot apply to a similar query or a
changed fixture implicitly. Context and extension outcomes are unscored.
"""
from itertools import groupby
import json


ISSUE = 'https://github.com/redis/redis-adbc-driver/issues/364'
POLICIES = {
    **{('issue-246.json', ident): {'kind': 'context', 'identity': identity, 'sql': sql}
       for ident, identity, sql in (
           ('row-1', 'version', 'select version()'),
           ('row-2', 'version', 'select pg_catalog.version()'),
           ('row-6', 'user', 'select current_user, session_user'),
           ('row-9', 'server_version', 'show server_version'),
           ('row-10', 'server_version_num', 'show server_version_num'),
       )},
    ('issue-268.json', 'repro-4'): {
        'kind': 'extension',
        'sql': "select date_add(timestamp '2024-01-01 00:00', interval 1 day)",
    },
    ('postgres/window.json', 'statement-0006'): {
        'kind': 'peers', 'columns': [3],
        'sql': 'SELECT depname, empno, salary, rank() OVER w FROM empsalary WINDOW w AS (PARTITION BY depname ORDER BY salary) ORDER BY rank() OVER w;',
    },
}


def policy_for(file, case):
    policy = POLICIES.get((file, case['id']))
    if policy is not None and case['sql'] != policy['sql']:
        raise ValueError(f'comparison policy source SQL changed: {file}:{case["id"]}')
    return {**policy, 'issue': ISSUE} if policy is not None else None


def identity_matches(pg, redis, policy, identities):
    """Verify actual endpoint contracts, never equate arbitrary identity text.

PostgreSQL expectations come from connection startup parameters/auth identity.
Redis advertises the driver's documented compatibility version, not the backend
Redis version and not the PostgreSQL 18 dialect objective.
"""
    if not identities:
        return False
    kind = policy['identity']
    width = 2 if kind == 'user' else 1
    for result in (pg, redis):
        if result.get('types') != ['string'] * width or len(result.get('rows', [])) != 1:
            return False
        if len(result['rows'][0]) != width or 'sqlstate' in result or 'decode_error' in result:
            return False
    if kind == 'version':
        text = pg['rows'][0][0]
        # Check the exact protocol-reported release, with PostgreSQL's build
        # suffix retained in raw output, rather than inventing a Redis build.
        return (isinstance(text, str)
                and text == identities['postgres_build']
                and text.startswith(f'PostgreSQL {identities["postgres_version"]} on ')
                and ', compiled by ' in text
                and redis['rows'] == [['PostgreSQL 18.0 (Redis ADBC driver)']])
    expected = {
        'user': ([identities['postgres_user']] * 2, [identities['redis_user']] * 2),
        'server_version': ([identities['postgres_version']], ['18.0']),
        'server_version_num': ([str(identities['postgres_version_num'])], ['180000']),
    }
    p, r = expected[kind]
    return pg['rows'] == [p] and redis['rows'] == [r]


def peer_rows(rows, columns):
    """Sort only adjacent equal-key peers; keep group order and multiplicity."""
    return [row for _, peers in groupby(rows, key=lambda r: [r[i] for i in columns])
            for row in sorted(peers, key=lambda r: json.dumps(r, sort_keys=True))]
