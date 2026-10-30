# Redis in dbc

Status (2026-10-29): packaging and catalog submission are prepared; Redis has
not been added to the default public registry by this change.

An [existing Redis onboarding discussion](https://github.com/orgs/adbc-drivers/discussions/9)
already covers this request. On October 26, the author linked the Redis-owned
repository, and a Foundry maintainer replied that they would set it up when
they had time. Onboarding is therefore already underway; do not create a
duplicate request. This confirms the discussion status, not live registry
availability. No follow-up was posted during this task.

[dbc](https://github.com/columnar-tech/dbc) discovers drivers from a hosted
[registry index](https://docs.columnar.tech/dbc/concepts/driver_registry/).
The production index is separate from the dbc CLI source. Adding a driver to a
documentation example does not register it.

The Foundry's [official onboarding process](https://adbc-drivers.org/building-drivers/#process)
directs driver authors to start a [GitHub Discussion](https://github.com/orgs/adbc-drivers/discussions)
or contact `hello@adbc-drivers.org`. Its
[distribution instructions](https://adbc-drivers.org/building-drivers/#distribution-packaging)
say a Foundry administrator checks release packages and uploads them to the
dbc CDN. A public catalog PR is therefore not a prerequisite for initiating
onboarding. Drivers can remain in their own organization; the Foundry also
offers a build-only repository for externally maintained drivers.

## Release packages and registry entry

The release workflow builds additional flat archives for Linux amd64/arm64 and
macOS arm64. Each contains `MANIFEST`, the shared library and `LICENSE`, with
an adjacent SHA-256 file. Existing native tarballs and wheels are still produced.
There are no Windows or macOS amd64 packages.

To build one package locally, run from the repository root:

```sh
python3 scripts/build_dbc_package.py \
  --library go/build/libadbc_driver_redis.dylib \
  --platform macos_arm64 --version v0.0.18 --out dist
python3 scripts/build_dbc_package.py --index-only --version v0.0.18 --out dist
```

Use the intended release version. The upload job generates `redis-index.yaml`
from the packages present, listing only available platforms with URLs under
`redis/redis-adbc-driver` GitHub releases. Existing releases do not acquire these
assets until they are explicitly rebuilt/published with this packaging support.

The proposed entry is [dbc/redis-index.yaml](dbc/redis-index.yaml). Its v0.0.18
URLs describe the packages to publish; those assets were not present in the
release when this proposal was prepared. Submit the generated release entry to
the public registry maintainers after the assets exist. The maintainers must
merge it into their catalog and arrange signatures under a key trusted by dbc
before the normal `dbc install redis` experience can work.

## Local installation before registry publication

These packages do not contain a Columnar signature. dbc's documented local
archive flow requires `--no-verify`. Copy the desired platform archive to
`redis.tar.gz` first because local installs derive the driver ID from the file
name:

```sh
cp dist/redis_macos_arm64-0.0.18.tar.gz /tmp/redis.tar.gz
dbc install --no-verify /tmp/redis.tar.gz
dbc list
```

Then load the driver using `driver="redis"`:

```python
from adbc_driver_manager import dbapi

with dbapi.connect(
    driver="redis",
    db_kwargs={"uri": "redis://localhost:6379/0"},
    autocommit=True,
) as connection:
    with connection.cursor() as cursor:
        cursor.execute("SELECT 1")
        print(cursor.fetch_arrow_table())
```

The server requirements in the [driver reference](driver-reference.md#server-requirements) apply:
Redis Query Engine/Search is required, and transactions and Flex are unsupported.

## Maintainer submission draft

Title: Add Redis ADBC driver to the public dbc registry

Please add the Redis ADBC driver under the ID `redis`.

- Source and documentation: https://github.com/redis/redis-adbc-driver
- License: Apache-2.0; publisher: ADBC Drivers Contributors.
- Platforms: `linux_amd64`, `linux_arm64`, `macos_arm64`.
- Minimum runtime: glibc 2.28 on Linux; macOS 12 on Apple Silicon.
- Entrypoint: `AdbcDriverRedisInit`; ADBC API version: 1.1.0.
- Release catalog: attach the generated `redis-index.yaml` after publishing its
  referenced packages and checksum files.
- The current v0.0.18 release contains native archives and Python wheels. The
  additional flat dbc packages and catalog are prepared locally and have not
  been published. We can provide those artifacts after packaging review.
- The packages currently have no Columnar signatures. Could a Foundry
  administrator help onboard this externally maintained driver, confirm the
  packaging/signing requirements, and arrange publication to the dbc CDN?

If a follow-up becomes necessary, use the
[existing Redis discussion](https://github.com/orgs/adbc-drivers/discussions/9)
and adapt this draft to acknowledge the maintainer's ongoing setup. Redis can
remain maintained at `redis/redis-adbc-driver`; no repository transfer is requested.

This draft has not been posted to GitHub or sent to maintainers.
