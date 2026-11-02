"""Offline checks for integration installation coverage and the publication gate."""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import tempfile
import unittest
import zipfile

import verify_release_wheels

ROOT = Path(__file__).resolve().parents[1]


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.args = argparse.Namespace(tag='v1.2.3', release=self.root / 'release.json',
                                       downloads=self.root / 'downloads', dist=self.root / 'dist')
        self.args.downloads.mkdir()
        assets = []
        for target in ('manylinux_2_28_x86_64', 'manylinux_2_28_aarch64', 'macosx_12_0_arm64'):
            name = f'adbc_driver_redis-1.2.3-py3-none-{target}.whl'
            native = bytearray(64)
            if target.startswith('manylinux'):
                native[:6] = b'\x7fELF\x02\x01'
                struct.pack_into('<H', native, 18, 183 if 'aarch64' in target else 62)
            else:
                native[:4] = b'\xcf\xfa\xed\xfe'
                struct.pack_into('<I', native, 4, 0x0100000C)
            with zipfile.ZipFile(self.args.downloads / name, 'w') as wheel:
                wheel.writestr('adbc_driver_redis/libadbc_driver_redis.so', native)
                wheel.writestr('adbc_driver_redis/sqlalchemy.py', '')
                wheel.writestr('adbc_driver_redis/superset.py', '')
                wheel.writestr('adbc_driver_redis-1.2.3.dist-info/METADATA',
                               'Name: adbc-driver-redis\nVersion: 1.2.3\nProvides-Extra: dbapi\n'
                               'Provides-Extra: sqlalchemy\nProvides-Extra: dagster\n')
            digest = self.checksum(name)
            assets.append({'name': name, 'digest': 'sha256:' + digest})
        self.release = {'tagName': 'v1.2.3', 'isDraft': False, 'isPrerelease': False, 'assets': assets}
        self.write_release()

    def checksum(self, name):
        digest = hashlib.sha256((self.args.downloads / name).read_bytes()).hexdigest()
        (self.args.downloads / (name + '.sha256')).write_text(digest + '  ' + name)
        return digest

    def write_release(self):
        self.args.release.write_text(json.dumps(self.release))

    def test_complete_release(self):
        verify_release_wheels.verify(self.args)
        self.assertEqual(len(list(self.args.dist.glob('*.whl'))), 3)

    def test_wheel_data_purelib_layout(self):
        # setuptools may place packages in .data/purelib for this py3-none wheel.
        name = next(p.name for p in self.args.downloads.glob('*arm64.whl'))
        path = self.args.downloads / name
        with zipfile.ZipFile(path) as wheel:
            files = {entry: wheel.read(entry) for entry in wheel.namelist()}
        with zipfile.ZipFile(path, 'w') as wheel:
            for entry, data in files.items():
                if not '.dist-info/' in entry:
                    entry = 'adbc_driver_redis-1.2.3.data/purelib/' + entry
                wheel.writestr(entry, data)
        digest = self.checksum(name)
        for asset in self.release['assets']:
            if asset['name'] == name:
                asset['digest'] = 'sha256:' + digest
        self.write_release()
        verify_release_wheels.verify(self.args)
        self.assertTrue((self.args.dist / name).is_file())

    def test_reject_missing_platform(self):
        next(self.args.downloads.glob('*.whl')).unlink()
        with self.assertRaisesRegex(AssertionError, 'Missing or unexpected'):
            verify_release_wheels.verify(self.args)
        self.assertFalse(self.args.dist.exists())

    def test_reject_corrupt_wheel(self):
        next(self.args.downloads.glob('*.whl')).write_bytes(b'corrupted')
        with self.assertRaisesRegex(AssertionError, 'Checksum mismatch'):
            verify_release_wheels.verify(self.args)
        self.assertFalse(self.args.dist.exists())

    def test_reject_draft_or_prerelease(self):
        for flag in ('isDraft', 'isPrerelease'):
            self.release[flag] = True
            self.write_release()
            with self.assertRaises(AssertionError):
                verify_release_wheels.verify(self.args)
            self.release[flag] = False

    def test_reject_architecture_even_with_valid_checksums(self):
        name = next(p.name for p in self.args.downloads.glob('*x86_64.whl'))
        path = self.args.downloads / name
        with zipfile.ZipFile(path) as wheel:
            files = {entry: wheel.read(entry) for entry in wheel.namelist()}
        header = bytearray(files['adbc_driver_redis/libadbc_driver_redis.so'])
        struct.pack_into('<H', header, 18, 183)
        files['adbc_driver_redis/libadbc_driver_redis.so'] = header
        with zipfile.ZipFile(path, 'w') as wheel:
            for entry, data in files.items():
                wheel.writestr(entry, data)
        digest = self.checksum(name)
        for asset in self.release['assets']:
            if asset['name'] == name:
                asset['digest'] = 'sha256:' + digest
        self.write_release()
        with self.assertRaises(ValueError):
            verify_release_wheels.verify(self.args)
        self.assertFalse(self.args.dist.exists())


class InstallationCoverageTests(unittest.TestCase):
    def test_every_current_inventory_row_has_an_installation_route(self):
        def rows(file):
            result = {}
            for line in file.read_text().splitlines():
                cells = line.split('|')
                if len(cells) > 3 and cells[1].strip().isdigit():
                    result[int(cells[1].strip())] = cells[2].strip()
            return result
        inventory = rows(ROOT / 'docs/data-engineering-adbc-support.md')
        installation = rows(ROOT / 'docs/integration-installation.md')
        self.assertTrue(inventory)
        self.assertEqual(installation, inventory)


if __name__ == '__main__':
    unittest.main()
