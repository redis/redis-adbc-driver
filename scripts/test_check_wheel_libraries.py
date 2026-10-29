import struct
import tempfile
import unittest
import zipfile
from pathlib import Path

from check_wheel_libraries import check_wheel


class WheelLibraryTests(unittest.TestCase):
    def check(self, target, content):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / f"adbc_driver_redis-0.1.0-py3-none-{target}.whl"
            with zipfile.ZipFile(path, "w") as wheel:
                wheel.writestr("adbc_driver_redis/libadbc_driver_redis.so", content)
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


if __name__ == "__main__":
    unittest.main()
