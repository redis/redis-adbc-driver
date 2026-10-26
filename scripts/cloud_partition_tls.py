#!/usr/bin/env python3
"""Validate partitions with database-scoped Cloud Pro ACL principals.

API credentials are a single JSON stdin line with `account` and `user` keys.
They stay in this process; native workers receive only their database identity.
This tool never provisions subscriptions. Use an explicitly owned disposable Pro
subscription containing exactly one database; the owner must delete it afterward.
"""
import argparse
import getpass
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import quote, urlsplit, urlunsplit

import redis


class UnknownOutcome(RuntimeError):
    """A mutation may have been accepted; never replay it automatically."""


class CloudAPI:
    def __init__(self, credentials):
        self.headers = {'x-api-key': credentials['account'], 'x-api-secret-key': credentials['user'],
                        'Content-Type': 'application/json', 'Accept': 'application/json',
                        'User-Agent': 'redis-adbc-partition-validation'}
        # Capture management trust before the runner selects its test CA bundle.
        self.context = ssl.create_default_context()
        self.pending = set()

    def call(self, method, path, body=None):
        request = urllib.request.Request('https://api.redislabs.com/v1/' + path,
            method=method, headers=self.headers,
            data=json.dumps(body).encode() if body is not None else None)
        try:
            with urllib.request.urlopen(request, timeout=30, context=self.context) as response:
                data = response.read()
                return json.loads(data) if data else {}
        except urllib.error.HTTPError as error:
            code = error.code
            error.close()
            # Cloud responses can echo passwords/keys. Never log their bodies.
            error_type = UnknownOutcome if method != 'GET' and code >= 500 else RuntimeError
            raise error_type(f'Cloud API {method} {path} failed (HTTP {code})') from None
        except (OSError, ValueError):
            error_type = UnknownOutcome if method != 'GET' else RuntimeError
            raise error_type(f'Cloud API {method} {path} response unavailable') from None

    def settle(self, task):
        deadline = time.monotonic() + 180
        while True:
            data = self.call('GET', 'tasks/' + task)
            status = data['status']
            if status == 'processing-completed':
                self.pending.discard(task)
                return data.get('response', {})
            if status == 'processing-error':
                self.pending.discard(task)
                raise RuntimeError('Cloud API task failed; inspect its task ID privately: ' + task)
            if time.monotonic() >= deadline:
                raise TimeoutError('Cloud API task did not settle: ' + task)
            time.sleep(1)

    def command(self, method, path, body=None):
        # Never replay a POST after an ambiguous response or a task failure.
        data = self.call(method, path, body)
        if data.get('taskId'):
            self.pending.add(data['taskId'])
            return self.settle(data['taskId'])
        return data


def wait_for(check, message):
    deadline = time.monotonic() + 180
    while not check():
        if time.monotonic() >= deadline:
            raise TimeoutError(message)
        time.sleep(1)


def verify_owned(api, config):
    if (config.get('disposable') is not True or
            not config['subscription_name'].startswith('codex-adbc-') or
            not config['database_name'].startswith('codex-adbc-')):
        raise ValueError('requires an explicitly owned disposable Cloud Pro fixture')
    sub = int(config['subscription_id'])
    db = int(config['database_id'])
    subscription = api.call('GET', f'subscriptions/{sub}')
    if subscription['name'] != config['subscription_name'] or subscription['status'] != 'active':
        raise ValueError('Cloud subscription ownership/status mismatch')
    listing = api.call('GET', f'subscriptions/{sub}/databases')
    databases = listing['subscription'][0]['databases']
    if len(databases) != 1 or databases[0]['databaseId'] != db:
        raise ValueError('Cloud fixture must contain exactly the owned database')
    database = api.call('GET', f'subscriptions/{sub}/databases/{db}')
    if (database['name'] != config['database_name'] or database['status'] != 'active'
            or database['memoryStorage'] != 'ram' or not database['security']['enableTls']):
        raise ValueError('Cloud database ownership/TLS/status mismatch')
    return database


def prepare(api, config, directory):
    database = verify_owned(api, config)
    subprocess.run(['bash', str(Path(__file__).with_name('create_partition_tls.sh')),
                    str(directory), '16379'], check=True)
    # Keep the generated local CA for the signed wrong-host negative check, and
    # add the normal trust roots used for the Cloud endpoint. No OS trust changes.
    ca = directory / 'ca.crt'
    roots = ssl.create_default_context().get_ca_certs(binary_form=True)
    with ca.open('a') as output:
        for root in roots:
            output.write(ssl.DER_cert_to_PEM_cert(root))
    endpoint = database['publicEndpoint']
    password = database['security']['password']
    (directory / 'admin.uri').write_text('rediss://default:' + quote(password, safe='') + '@' + endpoint + '/0')
    (directory / 'admin.password').write_text(password)
    config = {**{key: config[key] for key in ('disposable', 'subscription_id', 'database_id',
        'subscription_name', 'database_name')}, 'endpoint': endpoint, 'redis_version': database['redisVersion'],
              'shards': database['clustering']['numberOfShards']}
    (directory / 'cloud.json').write_text(json.dumps(config, indent=2) + '\n')


class CloudPrincipals:
    def __init__(self, fixture, schema, api, recover=False, minimum_shards=1):
        self.directory, self.schema, self.api = Path(fixture), schema, api
        raw = json.loads((self.directory / 'cloud.json').read_text())
        self.config = {key: raw[key] for key in ('disposable', 'subscription_id', 'database_id',
            'subscription_name', 'database_name', 'endpoint', 'redis_version', 'shards')}
        database = verify_owned(api, self.config)
        self.config['verified_eviction_policy'] = database.get('dataEvictionPolicy')
        self.config.update(verified_topology(database, minimum_shards))
        endpoint = urlsplit((self.directory / 'admin.uri').read_text().strip())
        if endpoint.netloc.split('@')[-1] != database['publicEndpoint']:
            raise ValueError('Cloud data endpoint does not match owned database')
        self.owned = []
        self.resource_ids = {}
        self.uncertain = set()
        self.people = {}
        journal = self.directory / 'cloud-acl-cleanup.json'
        if journal.exists():
            previous = json.loads(journal.read_text())
            if previous['status'] != 'passed' and not recover:
                raise ValueError('unfinished Cloud ACL journal; run cleanup before validating again')
            if recover:
                self.owned = [tuple(item) for item in previous['owned_principal_names']]
                self.resource_ids = previous.get('resource_ids', {})
                self.uncertain = {tuple(item) for item in previous.get('uncertain_creations', [])}
                self.api.pending.update(previous.get('pending_tasks', []))

    def journal(self, status='running', errors=None):
        record = {'status': status, 'owned_principal_names': self.owned,
            'resource_ids': getattr(self, 'resource_ids', {}),
            'uncertain_creations': sorted(self.uncertain),
            'pending_tasks': sorted(self.api.pending), 'errors': errors or []}
        # Preserve the previous complete ownership record if interrupted while
        # adding the next resource. This replacement finishes before POST.
        temporary = self.directory / 'cloud-acl-cleanup.tmp'
        with temporary.open('w') as output:
            json.dump(record, output, indent=2)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
        temporary.replace(self.directory / 'cloud-acl-cleanup.json')

    def find(self, kind, name):
        key = {'redisRules': 'redisRules', 'roles': 'roles', 'users': 'users'}[kind]
        matches = [item for item in self.api.call('GET', 'acl/' + kind)[key] if item['name'] == name]
        if len(matches) > 1:
            raise RuntimeError('Cloud ACL resource name is ambiguous')
        return matches[0] if matches else None

    def find_id(self, kind, uid):
        matches = [item for item in self.api.call('GET', 'acl/' + kind)[kind] if item['id'] == uid]
        if len(matches) > 1:
            raise RuntimeError('Cloud ACL resource ID is ambiguous')
        return matches[0] if matches else None

    def create_owned(self, kind, body):
        name = body['name']
        if self.find(kind, name):
            raise ValueError('Cloud ACL resource already exists; refusing to adopt it')
        # Register before POST: cleanup reconciles an ambiguous creation by exact
        # fresh name, after settling all known asynchronous tasks.
        self.owned.append((kind, name))
        self.uncertain.add((kind, name))
        self.journal()
        try:
            self.api.command('POST', 'acl/' + kind, body)
        except BaseException as error:
            if isinstance(error, Exception) and not isinstance(error, UnknownOutcome) and not self.api.pending:
                self.uncertain.discard((kind, name))
            self.journal()
            raise
        self.journal()
        result = None
        def ready():
            nonlocal result
            result = self.find(kind, name)
            return result is not None and result.get('status') == 'active'
        wait_for(ready, 'Cloud ACL resource did not become active')
        self.uncertain.discard((kind, name))
        self.resource_ids[kind + '/' + name] = result['id']
        self.journal()
        return result['id']

    def create(self, name, password, acl):
        rule_name, role_name = name + '-rule', name + '-role'
        rule = self.create_owned('redisRules', {'name': rule_name, 'redisRule': ' '.join(acl)})
        self.create_owned('roles', {'name': role_name, 'redisRules': [{'ruleName': rule_name,
            'databases': [{'subscriptionId': self.config['subscription_id'], 'databaseId': self.config['database_id']}]}]})
        self.create_owned('users', {'name': name, 'password': password, 'role': role_name})
        endpoint = urlsplit((self.directory / 'admin.uri').read_text().strip())
        uri = urlunsplit(('rediss', quote(name) + ':' + quote(password) + '@'
                         + endpoint.netloc.split('@')[-1], endpoint.path, '', ''))
        self.people[name] = {'acl': list(acl), 'rule': rule, 'rule_name': rule_name, 'uri': uri}
        def authenticated():
            client = self.client(name)
            try:
                return client.ping()
            except redis.AuthenticationError:
                return False
            finally:
                client.close()
        wait_for(authenticated, 'Cloud database principal did not authenticate')
        print('Cloud database principal authenticated: ' + ('planner' if name.startswith('planner_') else 'worker'), flush=True)

    def client(self, name):
        return redis.Redis.from_url(self.people[name]['uri'], ssl_ca_certs=str(self.directory / 'ca.crt'),
                                    ssl_cert_reqs='required', decode_responses=True, socket_timeout=10)

    def command(self, name, permission):
        person = self.people[name]
        if permission[1:] not in ('ft.info', 'hmget'):
            raise ValueError('unsupported Cloud ACL readiness probe')
        acl = person['acl'] + [permission]
        self.api.command('PUT', f"acl/redisRules/{person['rule']}",
                         {'redisRule': ' '.join(acl)})
        person['acl'] = acl
        probe = ('FT.INFO', 'idx:' + self.schema + ':rows') if permission[1:] == 'ft.info' else ('HMGET', self.schema + ':acl-probe', 'id')
        def propagated():
            client = self.client(name)
            try:
                client.execute_command(*probe)
                return permission.startswith('+')
            except redis.exceptions.NoPermissionError:
                return permission.startswith('-')
            finally:
                client.close()
        wait_for(propagated, 'Cloud worker ACL change did not propagate')

    def cleanup(self):
        errors = []
        for task in list(self.api.pending):
            try:
                self.api.settle(task)
            except Exception:
                errors.append('pending-task')
        for kind, name in reversed(self.owned):
            try:
                if (kind, name) in self.uncertain:
                    wait_for(lambda: self.find(kind, name) is not None,
                             'Cloud ACL creation outcome remains unknown')
                uid = getattr(self, 'resource_ids', {}).get(kind + '/' + name)
                resource = self.find_id(kind, uid) if uid is not None else self.find(kind, name)
                if resource:
                    uid = resource['id']
                    if not hasattr(self, 'resource_ids'):
                        self.resource_ids = {}
                    self.resource_ids[kind + '/' + name] = uid
                    self.journal()
                    self.api.command('DELETE', f'acl/{kind}/{uid}')
                    wait_for(lambda: self.find_id(kind, uid) is None, 'Cloud ACL deletion did not settle')
                self.uncertain.discard((kind, name))
            except Exception:
                errors.append(kind)
        self.journal('failed' if errors else 'passed', errors)
        if errors:
            raise RuntimeError('Cloud ACL cleanup failed: ' + ', '.join(errors))
        print('Owned Cloud ACL resources deleted; absence verified.', flush=True)


def verified_topology(database, minimum_shards):
    if minimum_shards < 1:
        raise ValueError('minimum shards must be positive')
    shards = database['clustering']['numberOfShards']
    if shards < minimum_shards:
        raise ValueError('Cloud fixture has fewer primary shards than required')
    return {'shards': shards, 'minimum_shards': minimum_shards,
            'oss_cluster_api': database.get('supportOSSClusterApi', False),
            'external_cluster_endpoints': database.get('useExternalEndpointForOSSClusterApi', False),
            'redis_version': database['redisVersion']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    prep = sub.add_parser('prepare')
    prep.add_argument('--config', type=Path, required=True)
    prep.add_argument('--directory', type=Path, required=True)
    validate = sub.add_parser('validate')
    validate.add_argument('--directory', type=Path, required=True)
    validate.add_argument('--output', type=Path, required=True)
    validate.add_argument('--require-wheel', action='store_true')
    validate.add_argument('--minimum-shards', type=int, default=1)
    validate.add_argument('--cluster-mode', choices=('auto', 'false', 'true'), default='auto')
    validate.add_argument('--minimum-cluster-primaries', type=int, default=1)
    cleanup = sub.add_parser('cleanup')
    cleanup.add_argument('--directory', type=Path, required=True)
    args = parser.parse_args()
    credentials = getpass.getpass('Cloud API credentials JSON (not echoed): ') if sys.stdin.isatty() else sys.stdin.readline()
    api = CloudAPI(json.loads(credentials))
    del credentials
    if args.command == 'prepare':
        prepare(api, json.loads(args.config.read_text()), args.directory)
    elif args.command == 'cleanup':
        CloudPrincipals(args.directory, None, api, recover=True).cleanup()
    else:
        import check_partition_tls
        # Neither API keys nor an administrative URI are placed in child env.
        os.environ['REDIS_TLS_ADMIN_URI'] = (args.directory / 'admin.uri').read_text().strip()
        os.environ['SSL_CERT_FILE'] = str(args.directory / 'ca.crt')
        options = ['--cloud-fixture', str(args.directory), '--output', str(args.output)]
        options += ['--cluster-mode', args.cluster_mode]
        options += ['--minimum-cluster-primaries', str(args.minimum_cluster_primaries)]
        if args.require_wheel:
            options.append('--require-wheel')
        check_partition_tls.main(options, cloud_principals_factory=
            lambda fixture, schema: CloudPrincipals(fixture, schema, api,
                minimum_shards=args.minimum_shards))


if __name__ == '__main__':
    main()
