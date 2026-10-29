# Building from source

Use this guide when developing the driver or building a platform-specific native
library. For a prebuilt release and sample dataset, use the
[quick start](../README.md#quick-start).

## Requirements

- Go 1.26.10 or newer and a C compiler with cgo support.
- Git and Make.
- Docker with Compose, `curl`, and `jq` for the local Redis Software database.
- [uv](https://docs.astral.sh/uv/) for the ADBC validation suite.

## Build the native driver

From the repository root:

```bash
make -C go build
```

The output is `go/build/libadbc_driver_redis.dylib` on macOS or
`go/build/libadbc_driver_redis.so` on Linux. To run the sample scripts against
your build, install the Python wrapper and set `REDIS_ADBC_LIBRARY` to its
absolute path (use `.dylib` on macOS):

```bash
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -e './python[dbapi]'
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so"
```

Start the disposable Redis database as described below before loading data.
The wrapper honors this override instead of looking for its bundled library.

To build the optional HASH collection adoption CLI:

```bash
make -C go adbc-cli
```

This produces `go/build/redis-adbc`. See
[existing HASH collections](driver-reference.md#existing-hash-collections-redis-adbc)
for usage.

## Start Redis and test

Use a disposable Redis Software database with Search and verified 4× QPF:

```bash
make -C go redis-up
REDIS_URI=redis://localhost:6379/0 make -C go test
REDIS_URI=redis://localhost:6379/0 make -C go validate
```

The test and validation suites modify the target database. See
[Building and testing](driver-reference.md#building-and-testing) for test scopes,
platform builds, and release details. For sample data and SQL examples, follow
[the README walkthrough](../README.md#3-load-the-sample-dataset).

When finished:

```bash
make -C go redis-down
```
