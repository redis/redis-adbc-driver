# Evidence

2026-10-22. Base: `590ab0a`. Client: macOS 26.7.1, arm64. Server: Redis Software
with Redis 8.6.2, three shards, dedicated local Docker fixture. Effective active
QPF 4x, six Search workers, `mnp` routing and 32 connections verified through
`make -C go redis-up`. Raw output and operational identifiers remain private.

## Sequential controls

Each valid run repeated `^TestSQLNaN(Joins|Text)$` 100 times: 200 test cases.
The existing assertions fetch and check query results. Tests and writes were
not retried by the investigation tooling.

| Client path | Go | Passed | Failed | Result |
|---|---|---:|---:|---|
| Host, localhost forwarding | 1.26.10 | 190 | 10 | EOFs in metadata reads and connection setup |
| Linux arm64, container network namespace loopback | 1.26.10 | 200 | 0 | No observed EOFs |
| Host, explicit IPv4 forwarding | 1.27.2 | 182 | 18 | EOFs reproduced |
| Host, explicit IPv4, corrected script | 1.26.10 | 184 | 16 | EOFs reproduced; 72.029 seconds |
| Linux arm64, Software container loopback, corrected script | 1.26.10 | 200 | 0 | No observed EOFs; 33.643 seconds |

The third run changes both address selection and Go version. It shows that the
failure also occurs with explicit IPv4 and the installed newer runtime; it
does not measure either variable's independent effect or compare failure rates
statistically. One earlier host/Linux attempt overlapped table-owning tests and
produced index/metadata interference; it was discarded and both paths rerun
sequentially. Its results are excluded above.

A separate read-only go-redis 9.23.0 control opened 5,000 fresh RESP3 clients,
issued PING and HGET of a nonexistent synthetic key, checked the nil reply and
closed each client, with command retries disabled. All 5,000 host iterations
passed in 5.727 seconds. This simpler workload did not reproduce the failure;
it does not rule out client/driver involvement in the failing SQL workload.

A private socket-metadata overlay on the repeated SQL run produced 182 passes
and 18 failures. All 73 traced EOF reads occurred before local Close, with zero
bytes received and exactly 22 bytes written (the initial HELLO 3 request).
Socket ages at EOF were 920–9,231 microseconds. None of those sockets reused a
connection tuple previously closed by that process. This localizes these
observations to the initial handshake, but does not identify who terminated
the connection. The overlay records lifecycle counters/timing, not packet
payloads; neither its raw output nor temporary source is published. Tuple
history is process-local and does not rule out forwarding state from prior runs.

Extending the original read-only go-redis control to 50,000 fresh clients yielded
10,723 passes and 39,277 failed PING attempts on the host in 62.511 seconds; the
matching Linux loopback control passed all 50,000 in 11.806 seconds. The first
control counted failed calls without classifying their errors, so these counts
alone are not evidence that every failed call was an EOF. Neither control
imports the ADBC driver. This motivates the reusable, classified probe below.

These controls strengthen the host-path association. The Linux run changes
client OS, scheduling and networking together; it is not proof of a particular
Docker forwarding defect. No connection termination initiator was identified.
Host packet capture was rejected by automatic approval review because it could
record Redis payloads/credentials; no packet-capture workaround was used.

## Reusable comparison

[software_eof_compare.py](../../../../scripts/software_eof_compare.py) requires
Python 3, Go, Docker, Make, curl and jq. Select an owned, dedicated Compose
project and unused local ports through the existing fixture environment
variables. Set `REDIS_URI` privately to its published endpoint. For example,
after setting those environment variables:

```sh
python3 scripts/software_eof_compare.py --output-dir /private/tmp/software-eof-comparison --count 100
```

The output directory must be new, outside the checkout, with an existing parent.
It is created with mode 0700. `GOTOOLCHAIN` defaults to `go1.26.10`; explicit
overrides are recorded in the summary. The script verifies the fixture before
each run, builds static binaries, runs sequentially, removes its copied Linux
binary and fails if either run fails or is incomplete. It leaves the fixture
running. Remove only the owned Compose project's Software service after use.
Do not run other integration tests against that database during comparison.

An initial scripted comparison also produced 18 host failures and 200 Linux
passes, but then failed to remove the root-owned binary created by `docker cp`.
Cleanup now uses container uid 0 for that exact, uniquely named file. The initial
failure was reported as a tooling error, not a passing comparison.

The corrected script completed both 200-case runs, wrote the [counts-only summary](comparison.json),
removed its copied binary successfully and exited **1**, preserving the host
test failure. The host log had 18 EOF-bearing lines for 16 failing tests; the
Linux log had zero. Summary checks covered complete passing, failing EOF and
incomplete output. Target guards rejected nonlocal hosts, mismatched fixture
ports, nonzero databases and URI query parameters before creating output or
starting a fixture. CLI help and local Markdown link checks passed.

## Standalone client-only reproduction

[software_eof_client.go](../../../../scripts/software_eof_client.go) uses only
go-redis and the Go standard library. After the same Software/QPF preflight,
set the fixture URI privately and run from the repository root:

```sh
GOTOOLCHAIN=go1.26.10 go -C go run ../scripts/software_eof_client.go -count 50000
```

It confirms RESP3 on every connection, disables command retries, checks PING and
a nil HGET of a synthetic nonexistent key, and closes the client on every
iteration. It emits a counts-only JSON summary and exits nonzero on a failed or
incomplete run. Its five-minute deadline bounds the diagnostic. It cannot
verify QPF independently; the fixture preflight remains mandatory. It reads
only, but must use the dedicated fixture. For the Linux control, cross-compile
this same file with `CGO_ENABLED=0`, `GOOS=linux` and the fixture's `GOARCH`, copy
the binary into the owned container, and run with that container's loopback
URI. Remove the copied root-owned binary using container uid 0 afterward.

The final client-only source, with explicit RESP3 confirmation and categorized
errors, completed all 50,000 [host iterations](client-host.json): 12,573 passed and **37,427 failed
with EOF** in 67.384 seconds. There were zero timeouts, other errors, read-phase
failures or close failures; it exited 1. These failures do not require the ADBC
driver. This does not by itself distinguish go-redis, the host stack, Docker
forwarding or the server proxy as the termination initiator. Nonlocal,
credentialed and nonzero-database target guards passed without revealing the
supplied credential text. Both host and Linux builds passed with Go 1.26.10;
gofmt passed.

The identical [Linux arm64 control](client-linux.json), run sequentially inside
the same Software container, passed **50,000/50,000** iterations with zero EOF,
timeout, other, read-phase or close failures in 12.505 seconds; it exited 0.
This establishes a client-only reproduction associated with the host path for
this fixture. It does not prove a specific Docker defect. The owned fixture was
removed after the investigation; raw logs and temporary tracing sources remain
private.

## Review and limits

Review against [REVIEW.md](../../../../REVIEW.md): no production driver changes,
retry additions, cancellation/timeout remapping or weakened SQL assertions.
The comparison is focused diagnostic evidence. A full Go suite and native
validation were not run for this tooling change. No claim that #498 is fixed,
that writes can safely be replayed, or that all Linux workloads pass.

Next useful evidence is a host/container correlated connection trace with
payloads excluded, or reproduction on a native Linux host. Preserve raw traces
privately and obtain appropriate capture authorization first. A production
transport change still requires a demonstrated failure mechanism and regression
case. See [intent](intent.md), [spec](spec.md), [plan](plan.md).
