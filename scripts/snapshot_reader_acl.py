"""Use the driver's canonical reader policy for owned native fixtures.

The fixture controller has Go available because it builds the native driver.
No URI, credential or descriptor is passed to the configuration-only CLI.
"""
import functools
import json
from pathlib import Path
import re
import subprocess


@functools.lru_cache(maxsize=128)
def reader_policy(domain):
    result = subprocess.run(
        ['go', 'run', './cmd/redis-adbc', 'reader-acl', '-domain', domain],
        cwd=Path(__file__).resolve().parents[1] / 'go',
        capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError('canonical snapshot reader policy generation failed')
    policy = json.loads(result.stdout)
    assert policy['format'] == 16
    assert re.fullmatch('[0-9a-f]{64}', policy['namespace'])
    return policy


def reader_rules(domain, prefixes=()):
    policy = reader_policy(domain)
    # Each ACL selector remains a single argument for protocol ACL SETUSER.
    rules = re.findall(r'\([^)]*\)|\S+', policy['acl'])
    for prefix in prefixes:
        assert re.fullmatch('[0-9a-f]{32}~', prefix)
        rules.append('%R~' + prefix + '*')
    return rules


def register_reader(control, domain):
    policy = reader_policy(domain)
    control.sadd(policy['registry'], policy['namespace'])


def unregister_reader(control, domain):
    policy = reader_policy(domain)
    control.srem(policy['registry'], policy['namespace'])


def access_prefix(physical):
    prefix = physical.split('~', 1)[0] + '~'
    assert re.fullmatch('[0-9a-f]{32}~', prefix)
    assert ':{meta}:snapshot:16:rows:' in physical
    return prefix
