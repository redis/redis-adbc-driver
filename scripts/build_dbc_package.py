#!/usr/bin/env python3
"""Build flat dbc packages and an index from the packages actually present."""

import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile


ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = "https://github.com/redis/redis-adbc-driver"
LIBRARIES = {
    "linux_amd64": "libadbc_driver_redis.so",
    "linux_arm64": "libadbc_driver_redis.so",
    "macos_arm64": "libadbc_driver_redis.dylib",
}


def normalize_version(value):
    value = value.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", value):
        raise ValueError(f"invalid release version: {value!r}")
    return value


def build_package(library, platform, version, out):
    version = normalize_version(version)
    library = Path(library)
    if library.name != LIBRARIES[platform]:
        raise ValueError(f"{platform} requires {LIBRARIES[platform]}")
    manifest = (
        'manifest_version = 1\nname = "Redis"\n'
        'publisher = "ADBC Drivers Contributors"\nlicense = "Apache-2.0"\n'
        f'version = "{version}"\n\n[ADBC]\nversion = "1.1.0"\n\n'
        '[Driver]\nentrypoint = "AdbcDriverRedisInit"\n\n'
        f'[Files]\ndriver = "{library.name}"\n'
    ).encode()
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    archive = out / f"redis_{platform}-{version}.tar.gz"
    with tarfile.open(archive, "w:gz") as package:
        header = tarfile.TarInfo("MANIFEST")
        header.size = len(manifest)
        header.mode = 0o644
        package.addfile(header, io.BytesIO(manifest))
        for source in (library, ROOT / "LICENSE"):
            package.add(source, arcname=source.name, recursive=False)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + ".sha256").write_text(
        f"{digest}  {archive.name}\n", encoding="utf-8"
    )
    return archive


def write_index(version, out):
    version = normalize_version(version)
    out = Path(out)
    packages = []
    for platform in LIBRARIES:
        filename = f"redis_{platform}-{version}.tar.gz"
        if (out / filename).is_file():
            packages.append({
                "platform": platform,
                "url": f"{REPOSITORY}/releases/download/v{version}/{filename}",
            })
    if not packages:
        raise ValueError("no dbc packages found; build packages before generating the index")
    entry = {
        "name": "Redis",
        "description": "An ADBC driver for Redis with SQL queries and Arrow bulk ingestion",
        "license": "Apache-2.0",
        "path": "redis",
        "urls": [REPOSITORY],
        "docs_url": f"{REPOSITORY}#readme",
        "pkginfo": [{"version": version, "packages": packages}],
    }
    # JSON is valid YAML and avoids a build-time YAML dependency.
    index = out / "redis-index.yaml"
    index.write_text(json.dumps({"drivers": [entry]}, indent=2) + "\n", encoding="utf-8")
    return index


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--out", type=Path, default=Path("dist"))
    parser.add_argument("--library", type=Path)
    parser.add_argument("--platform", choices=LIBRARIES)
    parser.add_argument("--index-only", action="store_true")
    args = parser.parse_args()
    if args.index_only:
        if args.library or args.platform:
            parser.error("--index-only cannot be combined with --library or --platform")
        print(write_index(args.version, args.out))
    else:
        if not args.library or not args.platform:
            parser.error("--library and --platform are required to build a package")
        print(build_package(args.library, args.platform, args.version, args.out))


if __name__ == "__main__":
    main()
