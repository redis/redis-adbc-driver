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
`go/build/libadbc_driver_redis.so` on Linux. The sample scripts load this path,
so the built library can replace the downloaded release in the quick start.

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
