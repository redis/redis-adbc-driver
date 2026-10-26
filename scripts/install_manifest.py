#!/usr/bin/env python3
"""Standalone tarball installer; manifest.py is copied alongside this file."""

import argparse
from pathlib import Path

from manifest import install_manifest

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--library", required=True)
parser.add_argument("--directory")
group = parser.add_mutually_exclusive_group()
for scope in ("user", "system", "venv"):
    group.add_argument("--" + scope, dest="scope", action="store_const", const=scope)
args = parser.parse_args()
version_file = Path(__file__).with_name("VERSION")
version = (
    version_file.read_text().strip().removeprefix("v")
    if version_file.exists()
    else None
)
for path in install_manifest(
    args.library, directory=args.directory, scope=args.scope or "user", version=version
):
    print(path)
