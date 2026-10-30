# Install, upgrade and uninstall the native driver

Use the managed native installer for DuckDB and other ADBC clients that load
`driver='redis'`. It detects your host, downloads a stable native release,
verifies its SHA-256 checksum and native loadability, and registers both `redis`
and `rediss` manifests. It needs Python 3.12+ and no Python packages. Installing
the native driver does not install the Python DB-API package or start Redis.

For Python applications, [install the wheel](setup.md#install-without-building)
instead. A Python wheel contains its own library; upgrading a managed native
installation does not upgrade that wheel.

## Install

From a checkout:

```sh
python3 scripts/install_driver.py install --version v0.0.18
```

Without a checkout, this single command retrieves and runs the same installer:

```sh
curl -fsSL https://raw.githubusercontent.com/redis/redis-adbc-driver/main/scripts/install_driver.py | python3 - install --version v0.0.18
```

To inspect it first, download `scripts/install_driver.py` from the repository
and run it locally. Replace the example tag with your chosen stable release.
Omit `--version` to resolve the latest published stable release. Downloads and
redirects use HTTPS; the checksum comes from the selected GitHub release.
The installer rejects prerelease tags, checksum mismatches, unsafe tar members,
unsupported targets and libraries that cannot load/export `AdbcDriverInit`.

Supported published targets are Linux x86-64/ARM64 with glibc 2.28+ and macOS
ARM64 with macOS 12+. Windows, macOS Intel and musl Linux are unsupported by
this installer. Native archives and checksums must exist for the selected tag.

No elevated privileges are requested. Default locations:

| Host | Stable library directory | Driver manifests |
|---|---|---|
| Linux | `$XDG_DATA_HOME/adbc/redis`, default `~/.local/share/adbc/redis` | `$XDG_CONFIG_HOME/adbc/drivers`, default `~/.config/adbc/drivers` |
| macOS | `~/Library/Application Support/ADBC/Redis` | `~/Library/Application Support/ADBC/Drivers` |

Use `--prefix` and `--manifest-directory` to choose other locations:

```sh
python3 scripts/install_driver.py install --version v0.0.18 \
  --prefix /path/to/native/redis --manifest-directory /path/to/manifests
export ADBC_DRIVER_PATH=/path/to/manifests
```

Pass `ADBC_DRIVER_PATH` to the actual client/service environment. When using
custom directories, always use the same `--prefix` for upgrade and uninstall;
the installer remembers the manifest directory. Do not move the prefix after
installation: manifests contain its absolute library path.

Existing Redis manifests are preserved by default: installation refuses to
replace them. Choose another manifest directory, or pass `--replace-manifests`
to back them up in the managed state and restore them on uninstall. Unmanaged
libraries already present in the prefix are never overwritten. The legacy
tarball `./install.sh` still registers a library where it was unpacked; it does
not create a managed installation for this upgrade/uninstall workflow.

## Upgrade

```sh
python3 scripts/install_driver.py upgrade
# Or pin a stable release:
python3 scripts/install_driver.py upgrade --version v0.0.18
```

If you used the checkout-free install, retrieve the script again or run the
same `curl ... | python3 - upgrade` command. The installer downloads and verifies
the selected release before replacing the library at its stable path. It updates
both manifests, preserves backups of any original manifests, and leaves no old
version directories. Selecting the installed version reinstalls that version;
downgrades require uninstalling first. Restart clients after an upgrade.

An installation lock rejects concurrent installer commands. Detected write
failures roll back the changed files; if rollback also fails, the diagnostic
identifies the preserved `.backup-*` directory, including `restore.json` mapping
backup files to their original destinations. Replacements are atomic per file,
not a multi-file transaction across crashes or power loss. If a process was
killed, ensure it has exited before removing `.install-lock`, inspect the managed
files and any remaining backup/staging directories, and restore from the backup
mapping where necessary. Do not remove `install-state.json`: it records ownership
and original manifests. The installer refuses to overwrite files changed by
another installation; resolve that conflict before retrying.

## Uninstall

```sh
python3 scripts/install_driver.py uninstall
# For a custom installation:
python3 scripts/install_driver.py uninstall --prefix /path/to/native/redis
```

Uninstall verifies managed file hashes, removes the managed library/header and
state, and removes its manifests or restores the manifests it replaced, including
their original permission modes. Modified manifests/libraries are left untouched
and cause an error. Unrelated files and directories are preserved. This does not
remove Python wheels, the `redis-adbc` adoption CLI, Redis data or Redis servers.

## Offline installation

Transfer the platform archive and its `.sha256` file from the same stable
[GitHub release](https://github.com/redis/redis-adbc-driver/releases):

```sh
python3 scripts/install_driver.py install --version v0.0.18 \
  --archive /downloads/adbc_driver_redis-v0.0.18-linux-amd64.tar.gz \
  --checksum-file /downloads/adbc_driver_redis-v0.0.18-linux-amd64.tar.gz.sha256
```

Both files and an explicit stable version are required. The checksum entry must
name the selected release archive (or contain only its digest). Offline upgrades
accept the same flags.

## Verify a connection

Run your native client's `SELECT 1`, then its application queries. If the Python
package is installed too, `doctor` can check both manifest names:

```sh
python -m adbc_driver_redis doctor --check-manifests
# A private CA for a rediss:// URI in REDIS_URI:
python -m adbc_driver_redis doctor --check-manifests --ca-file /path/to/ca.pem
```

The direct connection check uses the Python wheel's library; the manifest checks
use the managed native installation. Their versions may differ. The CA bundle
applies to both paths and preserves certificate and hostname verification. Use a
release/build containing `doctor --ca-file` for that option.
