# Native Linux benchmark client

Build the tested Go/Python client environment with:

```sh
docker build --tag redis-adbc-benchmark-client benchmarks/linux-client
```

This image contains Go 1.27.3, Python 3.14 and the pinned native benchmark
dependencies. It does not start a Redis server. Compile both compared Linux
driver libraries and CLI binaries before starting any timing windows. The
[write selection wrapper](../managed-writer-handoff/native_run.py) uses the
ordinary [table-mode scenarios](../table-modes/scenarios.py), including complete
typed results and independent final-state checks.

Run against an owned Redis Software database with verified active/effective 4×
QPF, six Search workers and noeviction. Supply endpoint credentials at runtime
through a private environment file, and mount the certificate-authority file for
certificate and hostname verification. Keep these files outside the image build
context and source control. A local client can share the existing Software
container's network namespace and address its TLS endpoint as `localhost` when
that name is covered by the server certificate. Git worktrees also need their Git
common directory mounted at the paths recorded in `.git`, so the harness can
verify each library's source revision.

Record client OS, architecture, CPU/memory limits, package versions and network
route alongside server resources and settings. Our 2026-11-14 standalone TLS
control reproduced a roughly 15 ms delay after large replies on the macOS
published-port route; the identical Go source against the unchanged server from
its Linux network namespace had no such cliff. OS and route changed together,
so this does not identify a particular faulty component. See the
[complete controls](../point-valid-isolation/README.md). Do not interpret their
GET/PING timings as SQL performance or relabel earlier measurements.
