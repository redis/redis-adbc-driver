"""Native installer lifecycle, integrity and failure recovery (no network/Redis)."""

import argparse
from contextlib import redirect_stdout, redirect_stderr
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import install_driver as installer


class NativeInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.prefix = self.root / 'library with spaces'
        self.manifests = self.root / 'manifest directory'
        self.target = patch.object(installer, 'host_target', return_value=('linux-amd64', 'linux_amd64', 'so'))
        self.target.start()
        self.addCleanup(self.target.stop)
        self.validation = patch.object(installer, 'validate_library')
        self.validation.start()
        self.addCleanup(self.validation.stop)

    def archive(self, version='v1.0.0', content=b'first library', members=None):
        root = f'adbc_driver_redis-{version}-linux-amd64'
        archive = self.root / (root + '.tar.gz')
        with tarfile.open(archive, 'w:gz') as tar:
            if members is None:
                members = [(f'{root}/libadbc_driver_redis.so', content, tarfile.REGTYPE),
                           (f'{root}/libadbc_driver_redis.h', b'header', tarfile.REGTYPE)]
            for name, data, kind in members:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.size = len(data) if kind == tarfile.REGTYPE else 0
                member.linkname = '/tmp/elsewhere' if kind != tarfile.REGTYPE else ''
                tar.addfile(member, io.BytesIO(data) if kind == tarfile.REGTYPE else None)
        checksum = archive.with_name(archive.name + '.sha256')
        checksum.write_text(f'{installer.digest(archive)}  {archive.name}\n')
        return archive, checksum

    def args(self, command='install', version='v1.0.0', content=b'first library'):
        archive, checksum = self.archive(version, content)
        return argparse.Namespace(command=command, prefix=self.prefix, version=version,
            manifest_directory=self.manifests, replace_manifests=False,
            archive=archive, checksum_file=checksum)

    def run_install(self, args):
        with redirect_stdout(io.StringIO()):
            installer.install(args)

    def run_uninstall(self):
        with redirect_stdout(io.StringIO()):
            installer.uninstall(argparse.Namespace(prefix=self.prefix))

    def test_install_upgrade_and_uninstall_stable_path(self):
        self.run_install(self.args())
        library = self.prefix / 'libadbc_driver_redis.so'
        manifest = self.manifests / 'redis.toml'
        self.assertIn(str(library), manifest.read_text())
        self.run_install(self.args('upgrade', 'v1.1.0', b'new library'))
        self.assertEqual(library.read_bytes(), b'new library')
        self.assertIn('version = "1.1.0"', manifest.read_text())
        self.assertIn(str(library), manifest.read_text())
        self.assertEqual(set(p.name for p in self.prefix.iterdir()),
            {'libadbc_driver_redis.so', 'libadbc_driver_redis.h', 'install-state.json'})
        self.run_uninstall()
        self.assertFalse(self.prefix.exists())
        self.assertFalse(manifest.exists())
        self.assertFalse((self.manifests / 'rediss.toml').exists())

    def test_existing_manifests_require_opt_in_and_are_restored(self):
        self.manifests.mkdir()
        (self.manifests / 'redis.toml').write_bytes(b'original manifest\n')
        (self.manifests / 'redis.toml').chmod(0o600)
        args = self.args()
        with self.assertRaisesRegex(ValueError, 'Existing Redis manifests'):
            self.run_install(args)
        args.replace_manifests = True
        self.run_install(args)
        self.run_install(self.args('upgrade', 'v1.1.0'))
        (self.prefix / 'unrelated.txt').write_text('keep')
        self.run_uninstall()
        self.assertEqual((self.manifests / 'redis.toml').read_bytes(), b'original manifest\n')
        self.assertEqual((self.manifests / 'redis.toml').stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.prefix / 'unrelated.txt').read_text(), 'keep')

    def test_tampered_files_and_manifests_are_not_deleted(self):
        for location in ('library', 'manifest'):
            with self.subTest(location=location):
                self.run_install(self.args())
                path = (self.prefix / 'libadbc_driver_redis.so' if location == 'library'
                        else self.manifests / 'redis.toml')
                original = path.read_bytes()
                path.write_bytes(b'changed by another installation')
                with self.assertRaisesRegex(ValueError, 'changed or removed'):
                    self.run_uninstall()
                with self.assertRaisesRegex(ValueError, 'changed or removed'):
                    self.run_install(self.args('upgrade', 'v1.1.0'))
                self.assertTrue((self.prefix / 'libadbc_driver_redis.so').exists())
                path.write_bytes(original)
                self.run_uninstall()

    def test_failed_upgrade_rolls_back_every_file(self):
        self.run_install(self.args())
        before = {path: path.read_bytes() for directory in (self.prefix, self.manifests)
                  for path in directory.iterdir()}
        original = installer.atomic_write
        failed = False

        def fail_once(path, content):
            nonlocal failed
            if path == self.manifests / 'rediss.toml' and not failed:
                failed = True
                raise OSError('simulated manifest write failure')
            return original(path, content)

        with patch.object(installer, 'atomic_write', side_effect=fail_once):
            with self.assertRaises(OSError):
                self.run_install(self.args('upgrade', 'v1.1.0', b'replacement'))
        self.assertEqual(before, {path: path.read_bytes() for path in before})
        self.assertFalse(list(self.prefix.glob('.backup-*')))
        self.run_uninstall()

    def test_failed_rollback_preserves_backups(self):
        self.prefix.mkdir()
        destination = self.prefix / 'file'
        destination.write_bytes(b'original')
        with patch.object(installer, 'atomic_write', side_effect=OSError('disk failure')):
            with self.assertRaisesRegex(ValueError, 'backups preserved'):
                installer.transaction(self.prefix, {destination: b'new'})
        backups = list(self.prefix.glob('.backup-*'))
        self.assertEqual(len(backups), 1)
        self.assertEqual((backups[0] / '0').read_bytes(), b'original')
        recovery = json.loads((backups[0] / 'restore.json').read_text())
        self.assertEqual(recovery[0]['path'], str(destination))

    def test_failed_first_install_removes_partial_registration(self):
        args = self.args()
        original = installer.atomic_write

        def fail(path, content):
            if path.name == 'rediss.toml':
                raise OSError('simulated registration failure')
            return original(path, content)

        with patch.object(installer, 'atomic_write', side_effect=fail):
            with self.assertRaises(OSError):
                self.run_install(args)
        self.assertFalse((self.prefix / 'libadbc_driver_redis.so').exists())
        self.assertFalse((self.prefix / 'install-state.json').exists())
        self.assertFalse((self.manifests / 'redis.toml').exists())

    def test_checksum_and_archive_names(self):
        args = self.args()
        args.archive.write_bytes(b'corrupted')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            self.run_install(args)
        self.assertFalse((self.manifests / 'redis.toml').exists())
        args = self.args()
        args.checksum_file.write_text(f'{installer.digest(args.archive)} other-release.tar.gz')
        with self.assertRaisesRegex(ValueError, 'filename does not match'):
            self.run_install(args)

    def test_unsafe_archive_members(self):
        for name, kind in [('../escape', tarfile.REGTYPE),
                           ('/tmp/escape', tarfile.REGTYPE),
                           ('adbc_driver_redis-v1.0.0-linux-amd64/link', tarfile.SYMTYPE),
                           ('adbc_driver_redis-v1.0.0-linux-amd64/link', tarfile.LNKTYPE)]:
            with self.subTest(name=name, kind=kind):
                args = self.args()
                args.archive, args.checksum_file = self.archive(members=[(name, b'bad', kind)])
                with self.assertRaisesRegex(ValueError, 'Unsafe'):
                    self.run_install(args)
        self.assertFalse((self.manifests / 'redis.toml').exists())

    def test_duplicate_and_missing_library(self):
        member = ('adbc_driver_redis-v1.0.0-linux-amd64/libadbc_driver_redis.so', b'lib', tarfile.REGTYPE)
        for members, message in [([member, member], 'Duplicate'), ([], 'missing')]:
            args = self.args()
            args.archive, args.checksum_file = self.archive(members=members)
            with self.assertRaisesRegex(ValueError, message):
                self.run_install(args)

    def test_unmanaged_library_and_symlink_manifest(self):
        args = self.args()
        self.prefix.mkdir()
        library = self.prefix / 'libadbc_driver_redis.so'
        library.write_bytes(b'not ours')
        with self.assertRaisesRegex(ValueError, 'Unmanaged'):
            self.run_install(args)
        library.unlink()
        self.manifests.mkdir()
        (self.manifests / 'redis.toml').symlink_to(self.root / 'not-present')
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.run_install(args)

    def test_upgrade_requires_install_and_rejects_downgrade(self):
        with self.assertRaisesRegex(ValueError, 'No managed installation'):
            self.run_install(self.args('upgrade'))
        self.run_install(self.args(version='v2.0.0'))
        with self.assertRaisesRegex(ValueError, 'Downgrades'):
            self.run_install(self.args('upgrade'))
        args = self.args('upgrade', 'v2.1.0')
        args.manifest_directory = self.root / 'other-manifests'
        with self.assertRaisesRegex(ValueError, 'original manifest directory'):
            self.run_install(args)

    def test_lock_rejects_concurrent_installation(self):
        with installer.locked(self.prefix):
            with self.assertRaisesRegex(ValueError, 'Another installer'):
                self.run_install(self.args())

    def test_https_release_download_selection(self):
        args = self.args(version='v1.2.3')
        source_archive, source_checksum = args.archive, args.checksum_file
        args.archive = args.checksum_file = None
        calls = []

        def download(url, path, limit=installer.MAX_DOWNLOAD):
            calls.append(url)
            path.write_bytes((source_checksum if url.endswith('.sha256') else source_archive).read_bytes())

        with patch.object(installer, 'download', side_effect=download):
            self.run_install(args)
        self.assertEqual(calls, [f'{installer.REPOSITORY}/releases/download/v1.2.3/{source_archive.name}.sha256',
                                 f'{installer.REPOSITORY}/releases/download/v1.2.3/{source_archive.name}'])

    def test_cli_requires_both_offline_inputs(self):
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as error:
            installer.main(['install', '--archive', 'file.tar.gz'])
        self.assertEqual(error.exception.code, 2)

    def test_redirects_cannot_downgrade_https(self):
        with self.assertRaisesRegex(ValueError, 'non-HTTPS'):
            installer.HTTPSRedirects().redirect_request(None, None, 302, '', {}, 'http://example.com/archive')

    def test_latest_resolves_to_a_stable_release(self):
        def download(url, path, limit):
            self.assertEqual(url, 'https://api.github.com/repos/redis/redis-adbc-driver/releases/latest')
            path.write_text(json.dumps({'tag_name': 'v1.2.3', 'draft': False, 'prerelease': False}))

        with patch.object(installer, 'download', side_effect=download):
            self.assertEqual(installer.resolve_version('latest', self.root), 'v1.2.3')
        with self.assertRaisesRegex(ValueError, 'stable tag'):
            installer.stable_version('v1.2.3;command')


if __name__ == '__main__':
    unittest.main()
