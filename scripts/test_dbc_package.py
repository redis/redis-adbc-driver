"""Check the artifact contract consumed by dbc, without running Redis."""

import hashlib
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from build_dbc_package import LIBRARIES, build_package, write_index


class DbcPackageTest(unittest.TestCase):
    def test_packages_and_index(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out = root / "dist"
            for platform, filename in LIBRARIES.items():
                with self.subTest(platform=platform):
                    library = root / filename
                    library.write_bytes(b"native-library-fixture")
                    archive = build_package(library, platform, "v0.0.18", out)
                    with tarfile.open(archive) as package:
                        self.assertEqual(set(package.getnames()), {"MANIFEST", filename, "LICENSE"})
                        self.assertTrue(all(member.isfile() for member in package))
                        manifest = package.extractfile("MANIFEST").read().decode()
                        self.assertIn('entrypoint = "AdbcDriverRedisInit"', manifest)
                        self.assertIn(f'driver = "{filename}"', manifest)
                        self.assertIn('version = "0.0.18"', manifest)
                        self.assertEqual(package.extractfile(filename).read(), library.read_bytes())
                    checksum = archive.with_name(archive.name + ".sha256").read_text()
                    self.assertEqual(checksum.split(), [hashlib.sha256(archive.read_bytes()).hexdigest(), archive.name])
            index = json.loads(write_index("v0.0.18", out).read_text())
            driver = index["drivers"][0]
            self.assertEqual(driver["path"], "redis")
            packages = driver["pkginfo"][0]["packages"]
            self.assertEqual({p["platform"] for p in packages}, set(LIBRARIES))
            for package in packages:
                self.assertTrue((out / package["url"].rsplit("/", 1)[1]).is_file())

    def test_index_only_lists_available_platforms(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaisesRegex(ValueError, "no dbc packages"):
                write_index("0.0.18", root)
            library = root / LIBRARIES["macos_arm64"]
            library.write_bytes(b"fixture")
            build_package(library, "macos_arm64", "0.0.18", root)
            index = json.loads(write_index("0.0.18", root).read_text())
            self.assertEqual(len(index["drivers"][0]["pkginfo"][0]["packages"]), 1)

    def test_reject_invalid_inputs(self):
        with self.assertRaisesRegex(ValueError, "invalid release version"):
            write_index("../../bad", Path("unused"))
        with self.assertRaisesRegex(ValueError, "requires"):
            build_package(Path("wrong.so"), "macos_arm64", "0.0.18", Path("unused"))


if __name__ == "__main__":
    unittest.main()
