"""Install name/URI discovery manifests with absolute shared-library paths."""

import json
import os
import platform
import sys
from pathlib import Path


def driver_directory(scope="user"):
    if scope == "venv":
        if sys.prefix == sys.base_prefix:
            raise ValueError("--venv requires an active virtual environment")
        return Path(sys.prefix) / "etc/adbc/drivers"
    if scope == "system":
        return Path(
            "/Library/Application Support/ADBC/Drivers"
            if sys.platform == "darwin"
            else "/etc/adbc/drivers"
        )
    if sys.platform == "darwin":
        return Path.home() / "Library/Application Support/ADBC/Drivers"
    return (
        Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
        / "adbc/drivers"
    )


def install_manifest(library, *, directory=None, scope="user", version=None):
    if version is None:
        from importlib.metadata import PackageNotFoundError
        from importlib.metadata import version as package_version

        try:
            version = package_version("adbc-driver-redis")
        except PackageNotFoundError:
            version = "unknown"
    library = Path(library).expanduser().resolve(strict=True)
    if not library.is_file():
        raise ValueError("library must be a file")
    arch = {
        "aarch64": "arm64",
        "arm64": "arm64",
        "x86_64": "amd64",
        "amd64": "amd64",
    }.get(platform.machine().lower())
    system = {"darwin": "macos", "linux": "linux"}.get(sys.platform)
    if not arch or not system:
        raise ValueError(
            "manifest installation supports Linux and macOS on amd64/arm64"
        )
    directory = Path(directory) if directory is not None else driver_directory(scope)
    directory.mkdir(parents=True, exist_ok=True)
    content = (
        f'manifest_version = 1\nname = "Redis"\nversion = {json.dumps(version)}\n'
        'publisher = "ADBC Drivers Contributors"\nlicense = "Apache-2.0"\n\n'
        '[ADBC]\nversion = "1.1.0"\n\n[Driver.shared]\n'
        f"{system}_{arch} = {json.dumps(str(library), ensure_ascii=False)}\n"
    )
    paths = [directory / f"{scheme}.toml" for scheme in ("redis", "rediss")]
    for path in paths:
        path.write_text(content, encoding="utf-8")
    return paths
