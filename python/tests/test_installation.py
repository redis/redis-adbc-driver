"""Installer checks independent of a running Redis server."""

import os
import shutil
import subprocess
import sys
from pathlib import Path

try:
    import tomllib
except ImportError:  # Python 3.10
    import tomli as tomllib

from adbc_driver_redis.manifest import driver_directory


def test_standalone_tarball_installer(tmp_path):
    root = Path(__file__).resolve().parents[2]
    release = tmp_path / 'release with spaces and "quotes"'
    release.mkdir()
    for name in ("install.sh", "install_manifest.py"):
        shutil.copy2(root / "scripts" / name, release)
    shutil.copy2(root / "python/adbc_driver_redis/manifest.py", release)
    library = release / "libadbc_driver_redis.so"
    library.write_bytes(b"a library path, not loaded by the installer")
    (release / "VERSION").write_text("v0.2.3\n")
    manifests = tmp_path / "manifests"
    # -S proves the tarball installer needs no site-packages (ADBC included).
    subprocess.run(
        [
            sys.executable,
            "-S",
            str(release / "install_manifest.py"),
            "--library",
            str(library),
            "--directory",
            str(manifests),
        ],
        check=True,
    )
    for scheme in ("redis", "rediss"):
        manifest = tomllib.loads((manifests / f"{scheme}.toml").read_text())
        assert manifest["version"] == "0.2.3"
        assert list(manifest["Driver"]["shared"].values()) == [str(library)]
    subprocess.run(
        [str(release / "install.sh"), "--directory", str(manifests)],
        env={**os.environ, "PYTHON": sys.executable},
        check=True,
    )


def test_manifest_destinations(tmp_path, monkeypatch):
    monkeypatch.setattr(sys, "platform", "linux")
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path))
    assert driver_directory() == tmp_path / "adbc/drivers"
    monkeypatch.setattr(sys, "prefix", str(tmp_path / "venv"))
    assert driver_directory("venv") == tmp_path / "venv/etc/adbc/drivers"
    monkeypatch.setattr(sys, "platform", "darwin")
    assert (
        driver_directory() == Path.home() / "Library/Application Support/ADBC/Drivers"
    )
