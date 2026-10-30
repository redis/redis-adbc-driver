#!/usr/bin/env python3
"""Install, upgrade or uninstall the native Redis ADBC driver (standard library only)."""

import argparse
import base64
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import urllib.request


REPOSITORY = "https://github.com/redis/redis-adbc-driver"
MAX_DOWNLOAD = 512 * 1024 * 1024


class HTTPSRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, newurl):
        if not newurl.startswith("https://"):
            raise ValueError("Download redirected to a non-HTTPS URL")
        return super().redirect_request(request, response, code, message, headers, newurl)


def host_target():
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    if sys.platform == "linux" and arch:
        libc, version = platform.libc_ver()
        if libc != "glibc" or tuple(map(int, version.split(".")[:2])) < (2, 28):
            raise ValueError("Linux releases require glibc 2.28+; musl is unsupported")
        return f"linux-{arch}", f"linux_{arch}", "so"
    if sys.platform == "darwin" and arch == "arm64":
        if int(platform.mac_ver()[0].split(".")[0]) < 12:
            raise ValueError("macOS releases require macOS 12+")
        return "darwin-arm64", "macos_arm64", "dylib"
    raise ValueError("Native releases support Linux amd64/arm64 and macOS arm64")


def user_manifest_directory():
    if sys.platform == "darwin":
        return Path.home() / "Library/Application Support/ADBC/Drivers"
    return Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config")) / "adbc/drivers"


def user_prefix():
    if sys.platform == "darwin":
        return Path.home() / "Library/Application Support/ADBC/Redis"
    return Path(os.environ.get("XDG_DATA_HOME", Path.home() / ".local/share")) / "adbc/redis"


def stable_version(value):
    if not re.fullmatch(r"v\d+\.\d+\.\d+", value):
        raise ValueError("Version must be a stable tag such as v0.0.18")
    return tuple(map(int, value[1:].split(".")))


def download(url, destination, limit=MAX_DOWNLOAD):
    request = urllib.request.Request(url, headers={"User-Agent": "redis-adbc-native-installer"})
    with urllib.request.build_opener(HTTPSRedirects()).open(request, timeout=30) as response, destination.open("wb") as output:
        if not response.url.startswith("https://"):
            raise ValueError("Download redirected to a non-HTTPS URL")
        total = 0
        while chunk := response.read(1024 * 1024):
            total += len(chunk)
            if total > limit:
                raise ValueError("Download exceeds the size limit")
            output.write(chunk)


def resolve_version(version, directory):
    if version == "latest":
        metadata = directory / "release.json"
        download("https://api.github.com/repos/redis/redis-adbc-driver/releases/latest", metadata, 1024 * 1024)
        release = json.loads(metadata.read_text())
        if release.get("draft") or release.get("prerelease"):
            raise ValueError("Expected a published stable release")
        version = release["tag_name"]
    stable_version(version)
    return version


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def unpack(archive, checksum, version, target, suffix, directory):
    filename = f"adbc_driver_redis-{version}-{target}.tar.gz"
    if archive.stat().st_size > MAX_DOWNLOAD or checksum.stat().st_size > 16 * 1024:
        raise ValueError("Archive/checksum exceeds the size limit")
    fields = checksum.read_text().split()
    if not fields or not re.fullmatch(r"[0-9a-fA-F]{64}", fields[0]):
        raise ValueError("Invalid SHA-256 checksum file")
    if len(fields) > 2 or (len(fields) == 2 and fields[1].lstrip("*") != filename):
        raise ValueError("Checksum filename does not match the selected release")
    if digest(archive) != fields[0].lower():
        raise ValueError("Release checksum mismatch; nothing installed")
    root = filename.removesuffix(".tar.gz")
    library = f"libadbc_driver_redis.{suffix}"
    extracted = {}
    with tarfile.open(archive, "r:gz") as tar:
        seen = set()
        total_size = 0
        for member in tar:
            total_size += member.size
            if len(seen) >= 256 or total_size > MAX_DOWNLOAD or member.size < 0:
                raise ValueError("Release archive exceeds unpacked size/member limits")
            path = PurePosixPath(member.name)
            if (path.is_absolute() or ".." in path.parts or not path.parts
                    or path.parts[0] != root or not (member.isfile() or member.isdir())):
                raise ValueError("Unsafe path or link in release archive")
            if member.name in seen:
                raise ValueError("Duplicate path in release archive")
            seen.add(member.name)
            if member.name not in (f"{root}/{library}", f"{root}/libadbc_driver_redis.h"):
                continue
            if not member.isfile() or not 0 < member.size <= MAX_DOWNLOAD:
                raise ValueError("Invalid native library/header size")
            destination = directory / path.name
            with tar.extractfile(member) as source, destination.open("wb") as output:
                shutil.copyfileobj(source, output)
            extracted[path.name] = destination
    if library not in extracted:
        raise ValueError("Selected release archive is missing the native library")
    return extracted


def validate_library(path, target):
    with path.open("rb") as source:
        header = source.read(64)
    if len(header) < 64:
        raise ValueError("Native library is truncated")
    if target.startswith("linux-"):
        machine = 62 if target.endswith("amd64") else 183
        valid = header[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", header, 18)[0] == machine
    else:
        valid = header[:4] == b"\xcf\xfa\xed\xfe" and struct.unpack_from("<I", header, 4)[0] == 0x0100000C
    if not valid:
        raise ValueError("Native library does not match this OS/architecture")
    check = subprocess.run([sys.executable, "-c", "import ctypes,sys; ctypes.CDLL(sys.argv[1]).AdbcDriverInit", str(path)], capture_output=True, timeout=30)
    if check.returncode:
        raise ValueError("Native library cannot load or lacks AdbcDriverInit; check OS/runtime compatibility")


@contextmanager
def locked(prefix):
    prefix.mkdir(parents=True, exist_ok=True)
    lock = prefix / ".install-lock"
    try:
        lock.mkdir()
    except FileExistsError:
        raise ValueError("Another installer may be running; inspect .install-lock before retrying") from None
    try:
        yield
    finally:
        lock.rmdir()


def read_state(prefix):
    path = prefix / "install-state.json"
    if path.is_symlink():
        raise ValueError("Installation state must not be a symlink")
    if not path.exists():
        return None
    state = json.loads(path.read_text())
    if state.get("format") != "redis-adbc-native-v1" or state.get("prefix") != str(prefix):
        raise ValueError("Unrecognized or moved installation state; use the original prefix")
    stable_version(state["version"])
    for name in state["files"]:
        if name not in ("libadbc_driver_redis.so", "libadbc_driver_redis.dylib", "libadbc_driver_redis.h"):
            raise ValueError("Unrecognized managed filename")
    for name in state["manifests"]:
        if name not in ("redis.toml", "rediss.toml"):
            raise ValueError("Unrecognized manifest filename")
    return state


def check_owned(prefix, state):
    for name, expected in state["files"].items():
        path = prefix / name
        if path.is_symlink() or not path.is_file() or digest(path) != expected:
            raise ValueError("Managed library/header was changed or removed; refusing to overwrite or uninstall")
    for name, record in state["manifests"].items():
        path = Path(state["manifest_directory"]) / name
        if path.is_symlink() or not path.is_file() or digest(path) != record["sha256"]:
            raise ValueError("Managed manifest was changed or removed; refusing to overwrite or uninstall")


def atomic_write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    mode = stat.S_IMODE(path.stat().st_mode) if path.exists() else 0o644
    if isinstance(content, tuple):
        content, mode = content
    if path.name == "install-state.json":
        mode = 0o600
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as output:
        temporary = Path(output.name)
        try:
            if isinstance(content, Path):
                with content.open("rb") as source:
                    shutil.copyfileobj(source, output)
            else:
                output.write(content)
            output.flush()
            os.fsync(output.fileno())
            os.chmod(temporary, mode)
            os.replace(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)


def transaction(prefix, changes):
    # Each replacement is atomic; roll back all completed operations on failure.
    # This is not a cross-directory transaction under power loss/SIGKILL.
    directory = Path(tempfile.mkdtemp(prefix=".backup-", dir=prefix))
    preserve_backup = False
    try:
        backups, touched = {}, []
        for index, path in enumerate(changes):
            if path.is_symlink() or (path.exists() and not path.is_file()):
                raise ValueError("Refusing to replace a symlink or non-file")
            backup = directory / str(index)
            backups[path] = backup if path.exists() else None
            if path.exists():
                shutil.copy2(path, backup)
        (directory / "restore.json").write_text(json.dumps([
            {"path": str(path), "backup": str(backup) if backup else None}
            for path, backup in backups.items()], indent=2) + "\n")
        try:
            for path, content in changes.items():
                touched.append(path)
                if content is None:
                    path.unlink(missing_ok=True)
                else:
                    atomic_write(path, content)
        except BaseException:
            failures = []
            for path in reversed(touched):
                try:
                    if backups[path] is None:
                        path.unlink(missing_ok=True)
                    else:
                        # Prefix and manifest directory may be on different disks.
                        atomic_write(path, (backups[path], stat.S_IMODE(backups[path].stat().st_mode)))
                except BaseException:
                    failures.append(path)
            if failures:
                preserve_backup = True
                raise ValueError(f"Rollback could not restore all files; backups preserved at {directory}") from None
            raise
    finally:
        if not preserve_backup:
            shutil.rmtree(directory)


def manifest_content(library, key, version):
    return (f'manifest_version = 1\nname = "Redis"\nversion = {json.dumps(version[1:])}\n'
            'publisher = "ADBC Drivers Contributors"\nlicense = "Apache-2.0"\n\n'
            '[ADBC]\nversion = "1.1.0"\n\n[Driver.shared]\n'
            f'{key} = {json.dumps(str(library))}\n').encode()


def install(args):
    target, key, suffix = host_target()
    prefix = args.prefix.expanduser().resolve()
    with locked(prefix), tempfile.TemporaryDirectory(prefix=".stage-", dir=prefix) as staging:
        directory = Path(staging)
        state = read_state(prefix)
        if args.command == "upgrade" and state is None:
            raise ValueError("No managed installation found; run install first")
        if state:
            check_owned(prefix, state)
            if state["target"] != target:
                raise ValueError("Installed target differs from this host")
        elif any((prefix / name).exists() or (prefix / name).is_symlink() for name in (
                "libadbc_driver_redis.so", "libadbc_driver_redis.dylib", "libadbc_driver_redis.h")):
            raise ValueError("Unmanaged library/header exists at this prefix; choose a new --prefix")
        if args.archive and args.version == "latest":
            raise ValueError("Local archives require an explicit --version")
        version = resolve_version(args.version, directory)
        if state and stable_version(version) < stable_version(state["version"]):
            raise ValueError("Downgrades are not supported; uninstall before installing an older version")
        manifests = (args.manifest_directory.expanduser().resolve() if args.manifest_directory else
                     Path(state["manifest_directory"]) if state else user_manifest_directory().expanduser().resolve())
        if state and str(manifests) != state["manifest_directory"]:
            raise ValueError("Upgrade must use the original manifest directory")
        records = {}
        for name in ("redis.toml", "rediss.toml"):
            path = manifests / name
            if path.is_symlink() or (path.exists() and not path.is_file()):
                raise ValueError("Manifest destination is a symlink or non-file")
            if state:
                previous = state["manifests"][name]["previous"]
                previous_mode = state["manifests"][name].get("previous_mode", 0o644)
            else:
                if path.exists() and not args.replace_manifests:
                    raise ValueError("Existing Redis manifests found; use a separate --manifest-directory or --replace-manifests to back up and replace them")
                if path.exists() and path.stat().st_size > 1024 * 1024:
                    raise ValueError("Existing manifest exceeds the backup size limit")
                previous = base64.b64encode(path.read_bytes()).decode() if path.exists() else None
                previous_mode = stat.S_IMODE(path.stat().st_mode) if path.exists() else None
            content = manifest_content(prefix / f"libadbc_driver_redis.{suffix}", key, version)
            records[name] = {"previous": previous, "previous_mode": previous_mode,
                             "sha256": hashlib.sha256(content).hexdigest()}
        filename = f"adbc_driver_redis-{version}-{target}.tar.gz"
        archive, checksum = args.archive, args.checksum_file
        if archive is None:
            archive, checksum = directory / filename, directory / (filename + ".sha256")
            url = f"{REPOSITORY}/releases/download/{version}/{filename}"
            download(url + ".sha256", checksum, 16 * 1024)
            download(url, archive)
        files = unpack(archive, checksum, version, target, suffix, directory)
        validate_library(files[f"libadbc_driver_redis.{suffix}"], target)
        changes = {prefix / name: source for name, source in files.items()}
        if state:
            changes.update({prefix / name: None for name in state["files"] if name not in files})
        for name in records:
            changes[manifests / name] = manifest_content(prefix / f"libadbc_driver_redis.{suffix}", key, version)
        new_state = {"format": "redis-adbc-native-v1", "prefix": str(prefix), "version": version,
                     "target": target, "files": {name: digest(path) for name, path in files.items()},
                     "manifest_directory": str(manifests), "manifests": records}
        changes[prefix / "install-state.json"] = (json.dumps(new_state, indent=2) + "\n").encode()
        transaction(prefix, changes)
    print(f"Installed Redis ADBC {version} ({target}) in {prefix}")
    print(f"Registered redis/rediss in {manifests}. Restart clients after upgrades.")
    if manifests != user_manifest_directory().expanduser().resolve():
        print(f"Set ADBC_DRIVER_PATH to {manifests} in your client's environment.")


def uninstall(args):
    prefix = args.prefix.expanduser().resolve()
    if not prefix.exists():
        raise ValueError("No managed installation found at this prefix")
    with locked(prefix):
        state = read_state(prefix)
        if state is None:
            raise ValueError("No managed installation found at this prefix")
        check_owned(prefix, state)
        changes = {prefix / name: None for name in state["files"]}
        for name, record in state["manifests"].items():
            changes[Path(state["manifest_directory"]) / name] = (
                (base64.b64decode(record["previous"], validate=True), record.get("previous_mode", 0o644))
                if record["previous"] is not None else None)
        changes[prefix / "install-state.json"] = None
        transaction(prefix, changes)
    # Leave any unrelated files/directories untouched.
    if not any(prefix.iterdir()):
        prefix.rmdir()
    print("Uninstalled managed Redis ADBC files; restored any replaced manifests.")


def main(argv=None):
    if sys.version_info < (3, 12):
        print("Native installer requires Python 3.12 or later.", file=sys.stderr)
        return 1
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("install", "upgrade", "uninstall"):
        sub = commands.add_parser(command)
        sub.add_argument("--prefix", type=Path, default=user_prefix(), help="Managed library directory (default: per-user data directory)")
        if command != "uninstall":
            sub.add_argument("--version", default="latest", help="Stable release tag, or latest (default)")
            sub.add_argument("--manifest-directory", type=Path, help="Custom manifest directory; set ADBC_DRIVER_PATH for clients")
            sub.add_argument("--replace-manifests", action="store_true", help="Back up existing manifests and restore them on uninstall")
            sub.add_argument("--archive", type=Path, help="Offline release archive (requires --checksum-file and pinned --version)")
            sub.add_argument("--checksum-file", type=Path)
    args = parser.parse_args(argv)
    if args.command != "uninstall" and bool(args.archive) != bool(args.checksum_file):
        parser.error("--archive and --checksum-file must be supplied together")
    try:
        (uninstall if args.command == "uninstall" else install)(args)
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.SubprocessError):
        # No native/server exceptions are printed. Actionable validation errors
        # raised by us contain no credentials; network errors are kept generic.
        error = sys.exc_info()[1]
        if type(error) is ValueError:
            print(f"Install failed: {error}", file=sys.stderr)
        else:
            print("Install failed: check release availability, network access, paths and permissions.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
