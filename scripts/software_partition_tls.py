#!/usr/bin/env python3
"""Owned Redis Software fixture management; never use against a shared cluster.

Bootstrap API access is localhost-only, using the existing disposable fixture's
self-signed management endpoint. After installing our API certificate, all API
and database requests verify the generated CA. No response bodies are logged.
"""
import argparse
import base64
import json
import os
import re
from pathlib import Path
import ssl
import subprocess
import time
import urllib.error
import urllib.request
from urllib.parse import quote, urlsplit, urlunsplit

import redis


class SoftwareAPI:
    def __init__(self, port, ca=None):
        if not 1 <= int(port) <= 65535:
            raise ValueError('invalid Software API port')
        self.base = f'https://localhost:{int(port)}/v1/'
        user = os.environ.get('SOFTWARE_ADMIN_USER', 'admin@example.com')
        password = os.environ.get('SOFTWARE_ADMIN_PASSWORD', 'adbc-software-test')
        self.auth = 'Basic ' + base64.b64encode((user + ':' + password).encode()).decode()
        self.context = ssl.create_default_context(cafile=str(ca)) if ca else ssl._create_unverified_context()

    def call(self, method, path, body=None):
        request = urllib.request.Request(self.base + path, method=method,
            data=json.dumps(body).encode() if body is not None else None,
            headers={'Authorization': self.auth, 'Content-Type': 'application/json'})
        deadline = time.monotonic() + 120
        while True:
            try:
                with urllib.request.urlopen(request, context=self.context, timeout=15) as response:
                    data = response.read()
                    return json.loads(data) if data else None
            except urllib.error.HTTPError as error:
                # Software rejects overlapping BDB reconfiguration with 409 even
                # briefly after status returns active. Only retry a rejected,
                # idempotent BDB PUT, never a principal-creating POST.
                if (error.code == 409 and method == 'PUT' and re.fullmatch(r'bdbs/[0-9]+', path)
                        and time.monotonic() < deadline):
                    error.close()
                    time.sleep(0.2)
                    continue
                # Response bodies can echo credentials, certificates or keys.
                code = error.code
                error.close()
                raise RuntimeError(f'Software API {method} {path} failed (HTTP {code})') from None



def wait_for(check, message):
    deadline = time.monotonic() + 120
    while not check():
        if time.monotonic() >= deadline:
            raise TimeoutError(message)
        time.sleep(0.2)


def prepare(args):
    directory = args.directory.resolve()
    container = json.loads(subprocess.check_output(['docker', 'inspect', args.container], text=True))[0]
    if container['Config'].get('Labels', {}).get('com.docker.compose.service') != 'redis-software':
        raise ValueError('requires the owned Compose Redis Software fixture')
    for internal, external in ((9443, args.api_port), (12000, args.db_port)):
        published = container['NetworkSettings']['Ports'].get(str(internal) + '/tcp')
        if published != [{'HostIp': '127.0.0.1', 'HostPort': str(external)}]:
            raise ValueError('Software fixture port ownership does not match localhost bindings')
    api = SoftwareAPI(args.api_port)
    databases = api.call('GET', 'bdbs')
    if len(databases) != 1:
        raise ValueError('requires the dedicated one-database Software fixture')
    bdb = databases[0]
    if (bdb['name'] != 'adbc-test' or bdb['shards_count'] != args.shards
            or bdb['port'] != 12000 or bdb.get('bigstore') or bdb['replication']
            or bdb['tls_mode'] != 'disabled' or bdb.get('roles_permissions')):
        raise ValueError('Software fixture does not match the fresh requested topology')
    subprocess.run(['bash', str(Path(__file__).with_name('create_partition_tls.sh')),
                    str(directory), str(args.db_port)], check=True)
    certificate, key = (directory / 'server.crt').read_text(), (directory / 'server.key').read_text()
    api.call('PUT', 'cluster/certificates', {'certificates': [
        {'name': name, 'certificate': certificate, 'key': key} for name in ('api', 'proxy')]})
    api = SoftwareAPI(args.api_port, directory / 'ca.crt')
    api.call('GET', 'cluster')  # Verify the new API certificate before more mutations.
    uid = bdb['uid']
    api.call('PUT', f'bdbs/{uid}', {'tls_mode': 'enabled', 'enforce_client_authentication': 'disabled',
        'authentication_redis_pass': (directory / 'admin.password').read_text().strip(),
        'search': {'search-max-aggregate-results': 137}})
    wait_for(lambda: api.call('GET', f'bdbs/{uid}')['status'] == 'active', 'Software TLS database did not become active')
    config = api.call('GET', f'bdbs/{uid}')
    assert config['tls_mode'] == 'enabled' and config['shards_count'] == args.shards
    def primaries_ready():
        shards = api.call('GET', f'bdbs/{uid}/shards')
        return len([s for s in shards if s['role'] == 'master' and s['status'] == 'active']) == args.shards
    wait_for(primaries_ready, 'Software primary shards did not become ready')

    control = redis.Redis.from_url((directory / 'admin.uri').read_text().strip(),
        ssl_ca_certs=str(directory / 'ca.crt'), ssl_cert_reqs='required', decode_responses=True)
    try:
        control.ping()
        control.execute_command('FT._LIST')
        assert control.config_get('search-max-aggregate-results')['search-max-aggregate-results'] == '137'
    finally:
        control.close()
    # Positive verified TLS succeeded above; plaintext must not get a Redis reply.
    plain = redis.Redis(host='127.0.0.1', port=args.db_port, socket_timeout=2, socket_connect_timeout=2)
    try:
        try:
            plain.ping()
        except (redis.ConnectionError, redis.TimeoutError):
            pass
        else:
            raise AssertionError('Software proxy accepted plaintext Redis')
    finally:
        plain.close()
    (directory / 'software.json').write_text(json.dumps({'api_port': args.api_port,
        'bdb_uid': uid, 'shards': args.shards, 'redis_version': config['version'],
        'image': container['Config']['Image'], 'image_id': container['Image'], 'tls_only': True, 'verified_management_api': True}, indent=2) + '\n')
    print('Verified Software API and TLS-only proxy ready; cap 137, requested primary shards.')


class SoftwarePrincipals:
    def __init__(self, fixture, schema=None):
        self.directory = Path(fixture).resolve()
        self.config = json.loads((self.directory / 'software.json').read_text())
        self.api = SoftwareAPI(self.config['api_port'], self.directory / 'ca.crt')
        database = self.api.call('GET', f"bdbs/{self.config['bdb_uid']}")
        self.config['verified_eviction_policy'] = database['eviction_policy']
        self.schema = schema
        self.resources = []
        self.people = {}

    def create(self, name, password, acl):
        role = self.api.call('POST', 'roles', {'name': name.split('@')[0], 'management': 'none'})['uid']
        self.resources.append(('roles', role))
        rule = self.api.call('POST', 'redis_acls', {'name': name.split('@')[0], 'acl': ' '.join(acl)})['uid']
        self.resources.append(('redis_acls', rule))
        path = f"bdbs/{self.config['bdb_uid']}"
        config = self.api.call('GET', path)
        mappings = config['roles_permissions'] + [{'role_uid': role, 'redis_acl_uid': rule}]
        self.api.call('PUT', path, {'roles_permissions': mappings})
        wait_for(lambda: self.api.call('GET', path)['status'] == 'active', 'Software ACL mapping did not settle')
        user = self.api.call('POST', 'users', {'email': name, 'name': name.split('@')[0],
            'password': password, 'role_uids': [role]})['uid']
        self.resources.append(('users', user))
        self.people[name] = {'acl': list(acl), 'rule': rule}
        endpoint = urlsplit((self.directory / 'admin.uri').read_text().strip())
        uri = urlunsplit(('rediss', quote(name) + ':' + quote(password) + '@'
                         + endpoint.netloc.split('@')[-1], endpoint.path, '', ''))
        self.people[name]['uri'] = uri
        def authenticated():
            client = self.client(name)
            try:
                return client.ping()
            except redis.AuthenticationError:
                return False
            finally:
                client.close()
        wait_for(authenticated, 'Software data principal did not become authenticated')

    def client(self, name):
        return redis.Redis.from_url(self.people[name]['uri'], ssl_ca_certs=str(self.directory / 'ca.crt'),
                                    ssl_cert_reqs='required', decode_responses=True)


    def command(self, name, permission):
        person = self.people[name]
        acl = person['acl'] + [permission]
        self.api.call('PUT', f"redis_acls/{person['rule']}", {'acl': ' '.join(acl)})
        person['acl'] = acl
        command = permission[1:].lower()
        if command == 'ft.info':
            probe = ('FT.INFO', 'idx:' + self.schema + ':rows')
        elif command == 'hmget':
            probe = ('HMGET', self.schema + ':acl-probe', 'id')
        else:
            raise ValueError('unsupported permission readiness probe')
        def propagated():
            client = self.client(name)
            try:
                client.execute_command(*probe)
                return permission.startswith('+')
            except redis.exceptions.NoPermissionError:
                return permission.startswith('-')
            finally:
                client.close()
        wait_for(propagated, 'Software worker ACL change did not propagate')


    def cleanup(self):
        errors = []
        roles = {uid for kind, uid in self.resources if kind == 'roles'}
        for kind, uid in reversed(self.resources):
            if kind != 'users':
                continue
            try:
                self.api.call('DELETE', f'{kind}/{uid}')
            except Exception:
                errors.append(kind)
        if roles:
            try:
                path = f"bdbs/{self.config['bdb_uid']}"
                config = self.api.call('GET', path)
                self.api.call('PUT', path, {'roles_permissions': [r for r in config['roles_permissions'] if r['role_uid'] not in roles]})
                wait_for(lambda: self.api.call('GET', path)['status'] == 'active', 'Software ACL cleanup did not settle')
            except Exception:
                errors.append('mappings')
        for kind, uid in reversed(self.resources):
            if kind == 'users':
                continue
            try:
                self.api.call('DELETE', f'{kind}/{uid}')
            except Exception:
                errors.append(kind)
        if errors:
            raise RuntimeError('Software principal cleanup failed: ' + ', '.join(errors))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--container', required=True)
    parser.add_argument('--api-port', type=int, required=True)
    parser.add_argument('--db-port', type=int, required=True)
    parser.add_argument('--shards', type=int, choices=(1, 3), required=True)
    prepare(parser.parse_args())
