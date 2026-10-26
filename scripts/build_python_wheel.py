#!/usr/bin/env python3
"""Build a native py3-none wheel from an already-built release library.

Requires build, setuptools and wheel. Does not compile, download, or publish anything.
"""

import argparse
import pathlib
import re
import shutil
import struct
import subprocess
import sys
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--library", type=pathlib.Path, required=True)
parser.add_argument("--platform", required=True)
parser.add_argument("--version", required=True)
parser.add_argument("--out", type=pathlib.Path, default=pathlib.Path("dist"))
args = parser.parse_args()
if args.platform not in (
    "manylinux_2_28_x86_64",
    "manylinux_2_28_aarch64",
) and not re.fullmatch(r"macosx_(?:1[2-9]|[2-9][0-9])_0_arm64", args.platform):
    parser.error(
        "supported targets: manylinux_2_28_{x86_64,aarch64}, macosx_<12 or later>_0_arm64"
    )
header = args.library.read_bytes()[:64]
if args.platform.startswith("manylinux_"):
    expected_machine = 183 if args.platform.endswith("aarch64") else 62
    if (
        header[:6] != b"\x7fELF\x02\x01"
        or struct.unpack_from("<H", header, 18)[0] != expected_machine
    ):
        parser.error(
            "library ELF architecture does not match the requested wheel platform"
        )
if args.platform.startswith("macosx_"):
    if (
        header[:4] != b"\xcf\xfa\xed\xfe"
        or struct.unpack_from("<I", header, 4)[0] != 0x0100000C
    ):
        parser.error("macOS wheels require a Mach-O arm64 library")
    commands = subprocess.check_output(["otool", "-l", str(args.library)], text=True)
    match = re.search(r"\bminos (\d+)(?:\.(\d+))?", commands)
    if not match:
        parser.error("cannot determine the Mach-O minimum OS version")
    minimum = (int(match[1]), int(match[2] or 0))
    target = tuple(map(int, args.platform.split("_")[1:3]))
    if minimum > target:
        parser.error(
            f"library requires macOS {minimum}; rebuild with MACOSX_DEPLOYMENT_TARGET matching {target}"
        )
source = pathlib.Path(__file__).resolve().parents[1] / "python"
out = args.out.resolve()
out.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory() as directory:
    package = pathlib.Path(directory) / "python"
    shutil.copytree(
        source,
        package,
        ignore=shutil.ignore_patterns(
            "__pycache__", "*.egg-info", "build", "dist", "*.so", "*.dylib", "*.dll"
        ),
    )
    # ADBC loads by absolute path, so the suffix may be .so on every platform.
    shutil.copy2(args.library, package / "adbc_driver_redis/libadbc_driver_redis.so")
    version = args.version.removeprefix("v")
    if version.endswith("-dev"):
        version = version[:-4] + ".dev0"
    from packaging.version import Version

    version = str(Version(version))
    pyproject = package / "pyproject.toml"
    pyproject.write_text(
        pyproject.read_text().replace('version = "0.1.0"', f'version = "{version}"')
    )
    # A C ABI library has no CPython ABI, but the wheel is platform-specific.
    (package / "setup.py").write_text(
        """from setuptools import setup
from setuptools.command.bdist_wheel import bdist_wheel
class NativeWheel(bdist_wheel):
    def finalize_options(self):
        super().finalize_options()
        self.root_is_pure = False
    def get_tag(self):
        return "py3", "none", self.plat_name
setup(cmdclass={"bdist_wheel": NativeWheel}, options={"bdist_wheel": {"plat_name": "PLATFORM"}})
""".replace("PLATFORM", args.platform)
    )
    subprocess.run(
        [
            sys.executable,
            "-m",
            "build",
            "--wheel",
            "--no-isolation",
            "--outdir",
            str(out),
        ],
        cwd=package,
        check=True,
    )
