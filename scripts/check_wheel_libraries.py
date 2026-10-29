#!/usr/bin/env python3
"""Check the native format/architecture of the library in a release wheel."""

import argparse
import struct
import zipfile
from pathlib import Path


def check_wheel(path):
    # The builder intentionally uses .so even for macOS Mach-O libraries.
    target = Path(path).name.removesuffix(".whl").rsplit("-", 1)[-1]
    with zipfile.ZipFile(path) as wheel:
        library = wheel.read("adbc_driver_redis/libadbc_driver_redis.so")
    if len(library) < 64:
        raise ValueError("Native library is truncated")
    if target in ("manylinux_2_28_x86_64", "manylinux_2_28_aarch64"):
        machine = 62 if target.endswith("x86_64") else 183
        valid = library[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", library, 18)[0] == machine
    elif target == "macosx_12_0_arm64":
        valid = library[:4] == b"\xcf\xfa\xed\xfe" and struct.unpack_from("<I", library, 4)[0] == 0x0100000C
    else:
        raise ValueError("Unsupported release wheel platform")
    if not valid:
        raise ValueError("Native library format/architecture does not match wheel platform")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("wheels", nargs="+", type=Path)
    for path in parser.parse_args().wheels:
        check_wheel(path)
        print(f"OK: {path.name}")
