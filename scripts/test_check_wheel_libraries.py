import struct
import tempfile
import unittest
import zipfile
from pathlib import Path

from check_wheel_libraries import check_wheel


class WheelLibraryTests(unittest.TestCase):
    def check(self, target, content, locations=None):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / f"adbc_driver_redis-0.1.0-py3-none-{target}.whl"
            with zipfile.ZipFile(path, "w") as wheel:
                for location in locations or ["adbc_driver_redis/libadbc_driver_redis.so"]:
                    wheel.writestr(location, content)
            check_wheel(path)

    def test_platforms_and_mislabeled_binaries(self):
        for target, magic, offset, fmt, machine in (
            ("manylinux_2_28_x86_64", b"\x7fELF\x02\x01", 18, "<H", 62),
            ("manylinux_2_28_aarch64", b"\x7fELF\x02\x01", 18, "<H", 183),
            ("macosx_12_0_arm64", b"\xcf\xfa\xed\xfe", 4, "<I", 0x0100000C),
        ):
            with self.subTest(target=target):
                content = bytearray(64)
                content[:len(magic)] = magic
                struct.pack_into(fmt, content, offset, machine)
                self.check(target, content)
                struct.pack_into(fmt, content, offset, 0)
                with self.assertRaisesRegex(ValueError, "does not match"):
                    self.check(target, content)

    def test_truncated_library(self):
        with self.assertRaisesRegex(ValueError, "truncated"):
            self.check("macosx_12_0_arm64", b"bad")

    def test_wheel_data_install_schemes(self):
        content = bytearray(64)
        content[:4] = b"\xcf\xfa\xed\xfe"
        struct.pack_into("<I", content, 4, 0x0100000C)
        for scheme in ("purelib", "platlib"):
            with self.subTest(scheme=scheme):
                self.check("macosx_12_0_arm64", content,
                    [f"adbc_driver_redis-0.1.0.data/{scheme}/adbc_driver_redis/libadbc_driver_redis.so"])

    def test_ambiguous_or_unrelated_library_locations(self):
        for locations in (
            ["unrelated/adbc_driver_redis/libadbc_driver_redis.so"],
            ["other-0.1.0.data/purelib/adbc_driver_redis/libadbc_driver_redis.so"],
            ["adbc_driver_redis/libadbc_driver_redis.so",
             "adbc_driver_redis-0.1.0.data/purelib/adbc_driver_redis/libadbc_driver_redis.so"],
        ):
            with self.subTest(locations=locations), self.assertRaisesRegex(ValueError, "exactly one"):
                self.check("macosx_12_0_arm64", bytes(64), locations)


if __name__ == "__main__":
    unittest.main()
