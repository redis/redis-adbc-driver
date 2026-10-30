import argparse
import os
import sys

from . import _driver_path
from .manifest import install_manifest


def main():
    parser = argparse.ArgumentParser(description="Redis ADBC installation and diagnostics")
    commands = parser.add_subparsers(dest="command", required=True)
    doctor = commands.add_parser("doctor", help="Check installation and optionally connect")
    doctor.add_argument("--uri", help="Redis URI (defaults to REDIS_URI; prefer the environment for credentials)")
    doctor.add_argument("--offline", action="store_true", help="Do not connect, even if REDIS_URI is set")
    doctor.add_argument("--check-manifests", action="store_true", help="Also test redis/rediss manifest discovery (requires a URI)")
    doctor.add_argument("--ca-file", help="PEM trust bundle for a verified rediss:// connection")
    install = commands.add_parser("install-manifest", help="Register the native library for other tools")
    group = install.add_mutually_exclusive_group()
    group.add_argument("--user", dest="scope", action="store_const", const="user")
    group.add_argument("--venv", dest="scope", action="store_const", const="venv")
    group.add_argument("--system", dest="scope", action="store_const", const="system")
    install.add_argument(
        "--directory", help="Custom manifest directory (add it to ADBC_DRIVER_PATH)"
    )
    install.add_argument("--library", help="Override bundled library path")
    args = parser.parse_args()
    if args.command == "doctor":
        from .doctor import diagnose

        uri = None if args.offline else args.uri or os.environ.get("REDIS_URI")
        if args.check_manifests and not uri:
            parser.error("--check-manifests requires --uri or REDIS_URI, without --offline")
        if args.ca_file and not uri:
            parser.error("--ca-file requires --uri or REDIS_URI, without --offline")
        return diagnose(uri=uri, check_manifests=args.check_manifests, ca_file=args.ca_file)
    for path in install_manifest(
        args.library or _driver_path(),
        directory=args.directory,
        scope=args.scope or "user",
    ):
        print(path)


if __name__ == "__main__":
    sys.exit(main())
