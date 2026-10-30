# Redis in dbc

Status (2026-10-29): v0.0.19 flat packages, checksums and the release catalog are
[published](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.19). Redis
is not yet listed in the default public registry; Foundry signing/CDN onboarding
is pending.

An [existing Redis onboarding discussion](https://github.com/orgs/adbc-drivers/discussions/9)
already covers this request. On October 26, the author linked the Redis-owned
repository, and a Foundry maintainer replied that they would set it up when
they had time. Onboarding is therefore already underway; do not create a
duplicate request. This confirms the discussion status, not live registry
availability. A [release follow-up](https://github.com/adbc-drivers/.github/discussions/9#discussioncomment-19283982)
was posted with the user's explicit authorization, linking the actual packages
and catalog for maintainer review.

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
  --platform macos_arm64 --version v0.0.19 --out dist
python3 scripts/build_dbc_package.py --index-only --version v0.0.19 --out dist
```

Use the intended release version. The upload job generates `redis-index.yaml`
from the packages present, listing only available platforms with URLs under
`redis/redis-adbc-driver` GitHub releases. Existing releases do not acquire these
assets until they are explicitly rebuilt/published with this packaging support.

The [released catalog](https://github.com/redis/redis-adbc-driver/releases/download/v0.0.19/redis-index.yaml)
and checked-in [catalog copy](dbc/redis-index.yaml) list the three actual v0.0.19
package URLs. All 27 release assets and checksum metadata were verified. The
maintainers must merge the entry into their catalog and arrange signatures under
a key trusted by dbc before the normal `dbc install redis` experience can work.

## Local installation before registry publication

These packages do not contain a Columnar signature. dbc's documented local
archive flow requires `--no-verify`. Copy the desired platform archive to
`redis.tar.gz` first because local installs derive the driver ID from the file
name:

```sh
cp dist/redis_macos_arm64-0.0.19.tar.gz /tmp/redis.tar.gz
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

## Maintainer handoff

The [v0.0.19 package/catalog update](https://github.com/adbc-drivers/.github/discussions/9#discussioncomment-19283982)
was posted to the existing onboarding discussion with explicit user authorization.
It requests review of the flat package format, signing, CDN publication and the
previously discussed build repository. Redis remains maintained at
`redis/redis-adbc-driver`; no repository transfer was requested.

The released macOS package was installed with dbc 0.3.1 into an isolated driver
directory, its manifest version and native entrypoint were verified through a
fully fetched SQL query against Redis Software, and it was uninstalled. This
establishes local package usability; it does not establish Columnar signatures
or default-registry availability. See [release validation](sdlc/changes/release-0019/validation.md).
