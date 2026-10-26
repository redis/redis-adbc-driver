#!/usr/bin/env python3
"""Create metadata-only post-release wheels, preserving every payload byte."""

import argparse
import base64
import csv
import email
import hashlib
import io
from pathlib import Path
import re
import tomllib
import zipfile


def repackage(wheel, version, revision, project, readme):
    destination_version = f"{version}.post{revision}"
    old_prefix = f"adbc_driver_redis-{version}.dist-info/"
    new_prefix = f"adbc_driver_redis-{destination_version}.dist-info/"
    destination = wheel.with_name(wheel.name.replace(f"-{version}-", f"-{destination_version}-", 1))
    if destination == wheel or destination.exists():
        raise ValueError(f"Unexpected or existing destination: {destination}")
    with zipfile.ZipFile(wheel) as source:
        metadata = email.message_from_bytes(source.read(old_prefix + "METADATA"))
        assert metadata["Name"] == "adbc-driver-redis" and metadata["Version"] == version
        metadata.replace_header("Version", destination_version)
        for field in ("Description-Content-Type", "Project-URL"):
            if field in metadata:
                del metadata[field]
        metadata["Description-Content-Type"] = "text/markdown"
        for label, url in project["urls"].items():
            metadata["Project-URL"] = f"{label}, {url}"
        metadata.set_payload(readme)
        records = []
        with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED) as target:
            for entry in source.infolist():
                if entry.filename == old_prefix + "RECORD":
                    continue
                if entry.filename.endswith(("/RECORD.jws", "/RECORD.p7s")):
                    raise ValueError("Cannot repackage signed wheels")
                data = source.read(entry.filename)
                name = entry.filename.replace(old_prefix, new_prefix, 1)
                if entry.filename == old_prefix + "METADATA":
                    data = metadata.as_bytes()
                entry.filename = name
                target.writestr(entry, data)
                digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
                records.append((name, f"sha256={digest}", str(len(data))))
            record_name = new_prefix + "RECORD"
            records.append((record_name, "", ""))
            record = io.StringIO(newline="")
            csv.writer(record, lineterminator="\n").writerows(records)
            target.writestr(record_name, record.getvalue())
    with zipfile.ZipFile(wheel) as source, zipfile.ZipFile(destination) as target:
        assert target.testzip() is None
        for name in source.namelist():
            if name not in (old_prefix + "METADATA", old_prefix + "RECORD"):
                assert source.read(name) == target.read(name.replace(old_prefix, new_prefix, 1)), name
    wheel.unlink()
    print(f"Created {destination.name}; all code and native library bytes unchanged")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--package", type=Path, default=Path("python"))
    args = parser.parse_args()
    if not re.fullmatch(r"\d+\.\d+\.\d+", args.version) or not re.fullmatch(r"[1-9]\d*", args.revision):
        parser.error("Expected a stable version and a positive integer post-release revision")
    project = tomllib.loads((args.package / "pyproject.toml").read_text())["project"]
    assert project["name"] == "adbc-driver-redis" and project["readme"] == "README.md"
    readme = (args.package / "README.md").read_text()
    assert readme.strip(), "Description must not be empty"
    wheels = sorted(args.dist.glob(f"adbc_driver_redis-{args.version}-*.whl"))
    assert len(wheels) == 3, "Expected all three release platform wheels"
    for wheel in wheels:
        repackage(wheel, args.version, args.revision, project, readme)


if __name__ == "__main__":
    main()
