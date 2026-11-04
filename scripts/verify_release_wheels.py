#!/usr/bin/env python3
"""Validate the complete release wheel set before immutable PyPI publication."""
import argparse
import email
import hashlib
import json
import pathlib
import re
import shutil
import zipfile

from check_wheel_libraries import check_wheel


def verify(args):
    tag = args.tag
    if not re.fullmatch(r'v\d+\.\d+\.\d+', tag):
        raise SystemExit('Expected a stable vMAJOR.MINOR.PATCH release tag')
    release = json.loads(args.release.read_text())
    assert release['tagName'] == tag and not release['isDraft'] and not release['isPrerelease']
    version = tag[1:]
    platforms = {'manylinux_2_28_x86_64', 'manylinux_2_28_aarch64', 'macosx_15_0_arm64'}
    expected = {f'adbc_driver_redis-{version}-py3-none-{p}.whl' for p in platforms}
    root = args.downloads
    assert {p.name for p in root.glob('*.whl')} == expected, 'Missing or unexpected wheel'
    assets = {a['name']: a for a in release['assets']}
    verified = []
    for name in sorted(expected):
        wheel = root / name
        digest = hashlib.sha256(wheel.read_bytes()).hexdigest()
        checksum = (root / (name + '.sha256')).read_text().split()[0]
        assert digest == checksum, f'Checksum mismatch: {name}'
        api_digest = assets[name].get('digest')
        if api_digest:
            assert api_digest == 'sha256:' + digest, f'GitHub digest mismatch: {name}'
        with zipfile.ZipFile(wheel) as z:
            assert z.testzip() is None
            metadata_path, = [n for n in z.namelist() if n.endswith('.dist-info/METADATA')]
            metadata = email.message_from_bytes(z.read(metadata_path))
            assert metadata['Name'] == 'adbc-driver-redis' and metadata['Version'] == version
            assert {'dbapi', 'sqlalchemy', 'dagster'} <= set(metadata.get_all('Provides-Extra', []))
            assert any(n.endswith('adbc_driver_redis/superset.py') for n in z.namelist())
            assert any(n.endswith('adbc_driver_redis/sqlalchemy.py') for n in z.namelist())
        check_wheel(wheel)
        verified.append(wheel)
        print(name, digest)
    args.dist.mkdir(exist_ok=False)
    for wheel in verified:
        shutil.copy2(wheel, args.dist / wheel.name)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tag', required=True)
    parser.add_argument('--release', type=pathlib.Path, default=pathlib.Path('release.json'))
    parser.add_argument('--downloads', type=pathlib.Path, default=pathlib.Path('downloads'))
    parser.add_argument('--dist', type=pathlib.Path, default=pathlib.Path('dist'))
    verify(parser.parse_args())
