#!/usr/bin/env python3
"""Smoke-test real native load, manifest registration, upgrade and uninstall."""

import argparse
import os
from pathlib import Path
import tarfile
import tempfile

import install_driver


def check(library, check_connection=False, ca_file=None):
    target, _, suffix = install_driver.host_target()
    with tempfile.TemporaryDirectory(prefix='redis-native-lifecycle-') as directory:
        directory = Path(directory).resolve()
        prefix, manifests = directory / 'install', directory / 'manifests'
        for command, version in [('install', 'v0.0.1'), ('upgrade', 'v0.0.2')]:
            root = f'adbc_driver_redis-{version}-{target}'
            archive = directory / (root + '.tar.gz')
            with tarfile.open(archive, 'w:gz') as tar:
                tar.add(library, arcname=f'{root}/libadbc_driver_redis.{suffix}')
            checksum = archive.with_name(archive.name + '.sha256')
            checksum.write_text(f'{install_driver.digest(archive)}  {archive.name}\n')
            args = [command, '--version', version, '--prefix', str(prefix),
                    '--manifest-directory', str(manifests), '--archive', str(archive),
                    '--checksum-file', str(checksum)]
            if install_driver.main(args):
                raise AssertionError('Native lifecycle install/upgrade failed')
            state = install_driver.read_state(prefix)
            assert state['version'] == version
            installed = prefix / f'libadbc_driver_redis.{suffix}'
            assert install_driver.digest(installed) == install_driver.digest(library)
            install_driver.check_owned(prefix, state)
            assert str(installed) in (manifests / 'redis.toml').read_text()
            assert (manifests / 'redis.toml').read_bytes() == (manifests / 'rediss.toml').read_bytes()
            if check_connection:
                import adbc_driver_manager.dbapi as manager

                old_path = os.environ.get('ADBC_DRIVER_PATH')
                os.environ['ADBC_DRIVER_PATH'] = str(manifests)
                options = {'uri': os.environ['REDIS_URI']}
                if ca_file:
                    options['adbc.redis.tls_ca_file'] = str(ca_file)
                try:
                    for driver in ('redis', 'rediss'):
                        with manager.connect(driver=driver, db_kwargs=options, autocommit=True) as conn, conn.cursor() as cursor:
                            cursor.execute('SELECT 1')
                            assert cursor.fetchall() == [(1,)]
                finally:
                    if old_path is None:
                        os.environ.pop('ADBC_DRIVER_PATH', None)
                    else:
                        os.environ['ADBC_DRIVER_PATH'] = old_path
        if install_driver.main(['uninstall', '--prefix', str(prefix)]):
            raise AssertionError('Native lifecycle uninstall failed')
        assert not prefix.exists()
        assert not list(manifests.iterdir())
    print('PASS: real native load, stable paths, manifest registration, upgrade and uninstall')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--library', required=True, type=Path)
    parser.add_argument('--check-connection', action='store_true', help='Fetch SELECT 1 via both manifests using REDIS_URI')
    parser.add_argument('--ca-file', type=Path)
    args = parser.parse_args()
    check(args.library.resolve(strict=True), args.check_connection, args.ca_file)
