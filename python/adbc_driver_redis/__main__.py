import argparse

from . import _driver_path
from .manifest import install_manifest


def main():
    parser = argparse.ArgumentParser(description="Install Redis ADBC driver manifests")
    parser.add_argument("command", choices=["install-manifest"])
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--user", dest="scope", action="store_const", const="user")
    group.add_argument("--venv", dest="scope", action="store_const", const="venv")
    group.add_argument("--system", dest="scope", action="store_const", const="system")
    parser.add_argument(
        "--directory", help="Custom manifest directory (add it to ADBC_DRIVER_PATH)"
    )
    parser.add_argument("--library", help="Override bundled library path")
    args = parser.parse_args()
    for path in install_manifest(
        args.library or _driver_path(),
        directory=args.directory,
        scope=args.scope or "user",
    ):
        print(path)


if __name__ == "__main__":
    main()
