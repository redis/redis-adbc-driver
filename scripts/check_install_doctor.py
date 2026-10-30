#!/usr/bin/env python3
"""Exercise doctor against an owned Software TLS fixture; emit no credentials."""

import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit, urlunsplit
import uuid

from software_partition_tls import SoftwarePrincipals, qpf_4x_ready


def run_checks(fixture, output, no_search_uri=None):
    from adbc_driver_redis import _driver_path
    from adbc_driver_redis.manifest import install_manifest

    fixture = fixture.resolve()
    principals = SoftwarePrincipals(fixture)
    config = principals.api.call('GET', f"bdbs/{principals.config['bdb_uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
    if not qpf_4x_ready(config):
        raise AssertionError('Software fixture lacks verified effective 4x QPF')
    uri = (fixture / 'admin.uri').read_text().strip()
    ca = fixture / 'ca.crt'
    results = []
    with tempfile.TemporaryDirectory(prefix='redis-doctor-manifests-') as directory:
        manifests = Path(directory)
        install_manifest(_driver_path(), directory=manifests)

        def check(name, endpoint, expected, message, *, trust=ca, discovery=False, overrides=None):
            env = {**os.environ, 'REDIS_URI': endpoint, 'ADBC_DRIVER_PATH': str(manifests)}
            env.update(overrides or {})
            command = [sys.executable, '-m', 'adbc_driver_redis', 'doctor']
            if trust is not None:
                command += ['--ca-file', str(trust)]
            if discovery:
                command.append('--check-manifests')
            process = subprocess.run(command, env=env, capture_output=True, text=True, timeout=90)
            combined = process.stdout + process.stderr
            if process.returncode != expected or message not in combined:
                raise AssertionError(f'{name}: unexpected diagnostic/exit status (raw output withheld)')
            password = urlsplit(endpoint).password
            if endpoint in combined or (password and password in combined):
                raise AssertionError(f'{name}: credential appeared in diagnostic output')
            results.append({'name': name, 'exit_code': process.returncode, 'passed': True})
            print(f'PASS: {name}')

        try:
            check('verified Software TLS and both manifests', uri, 0, "driver='rediss' discovery", discovery=True)
            check('untrusted CA rejected', uri, 1, 'TLS failed', trust=fixture / 'untrusted.crt')
            parts = urlsplit(uri)
            wrong = urlunsplit((parts.scheme, 'default:' + secrets.token_hex(24) + '@' + parts.netloc.split('@')[-1], parts.path, '', ''))
            check('invalid authentication rejected', wrong, 1, 'Authentication failed')
            name = 'doctor-' + uuid.uuid4().hex[:12] + '@example.com'
            principals.create(name, secrets.token_hex(24), ['on', '~*', '+@all', '-ft._list'])
            check('Search ACL denied', principals.people[name]['uri'], 1, 'denied a command')
            (manifests / 'redis.toml').write_text('manifest_version = 1\n[Driver.shared]\nmacos_arm64 = "/missing/driver"\nlinux_amd64 = "/missing/driver"\nlinux_arm64 = "/missing/driver"\n')
            check('broken manifest rejected', uri, 1, 'redis discovery/query failed', discovery=True)
            check('missing native library rejected', uri, 1, 'Native library missing', overrides={'REDIS_ADBC_LIBRARY': str(manifests / 'absent-library')})
            if no_search_uri:
                check('server without Search rejected', no_search_uri, 1, 'Search is unavailable', trust=None)
        finally:
            principals.cleanup()
    evidence = {'distribution': 'Redis Software', 'redis_version': principals.config['redis_version'],
                'query_performance_factor': config['query_performance_factor'],
                'search_workers': config['search']['search-workers'], 'sched_policy': config['sched_policy'],
                'conns': config['conns'], 'checks': results}
    output.write_text(json.dumps(evidence, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--software-fixture', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--no-search-uri', help='Optional dedicated negative fixture without Search')
    args = parser.parse_args()
    run_checks(args.software_fixture, args.output, args.no_search_uri)
