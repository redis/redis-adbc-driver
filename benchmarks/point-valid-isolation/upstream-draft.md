# Draft: next TLS response waits after a moderately large preceding reply

Status: prepared for coordinator review, not submitted. Independent RedisPy
reproduced the host-path cliff, while the identical Go source on a Linux client
in the server network namespace did not. Triage the actual host/network path
before assigning an upstream client/server project. No infrastructure identities,
credentials, arguments or application data are included.

## Observed behavior

On the verified Redis Software TLS fixture, a pipelined GET of 8,000 bytes is
followed by a PING taking about 0.2–0.3 ms. GET payloads of 8,191/8,192/8,193,
16,384 and 32,768 bytes are followed by first PING medians around 14–16 ms;
following PINGs return quickly. The exact boundary has not been determined.
Eight observations per case/route are retained, not confidence intervals.

The [standalone Go reproducer](repro/main.go) uses go-redis v9.23.0, RESP3 and
certificate/hostname-verified TLS. It reproduces without ADBC, SQL, Search, Lua,
managed tables, Arrow, native driver code or shared statement lifetimes. It
checks every complete GET value and PONG, uses no-retry commands, and removes
only its own expiring random control keys. It does not run FLUSHDB or SCAN.

The effect remains when the next PING uses the same proof client, a separate
client, scalar dispatch, or a fresh one-command pipeline. It also remains with
the existing native `pipeline_pool_size=-1` option: both dedicated pipeline
pools and regular-pool-only operation reproduce. The driver does not configure
an 8 KiB reader: native option defaults are 32 KiB regular reads and 128 KiB
dedicated pipeline reads.

Earlier pin/native-index diagnostics recorded zero pool waits and response
socket-read elapsed tracking the long first response interval. Read-only cgroup
counters around one complete short invocation recorded no CPU-quota throttling;
that does not establish absence of all host/server scheduling effects. The
container had a four-CPU quota/8 GiB memory and the Docker VM reported 18 CPUs.
The topology includes host-to-container port forwarding and TLS, so that boundary
still needs independent localization. No server configuration was changed.

This is a response-wait observation, not proof of server CPU, delayed ACK/Nagle,
TLS implementation, go-redis or Redis Software root cause. An earlier SLOWLOG
observation had no threshold-selected sample; server execution time is unavailable.

## Reproduce from this repository's Go module

Build once before running timed sequences:

```sh
cd go
go build -o /private/path/reply-route-repro ../benchmarks/point-valid-isolation/repro/main.go
```

Set `REDIS_BENCH_URI` and `SSL_CERT_FILE` privately. Use an exclusively owned,
disposable Redis Software endpoint with the project's independently verified
active/effective 4× QPF configuration. Run the persistent binary through that
verification gate; keep endpoints and CA/configuration files out of public logs.

The program reports all observations, dependency version and effective pool
topology without endpoint/key identifiers. Its only writes are synthetic
expiring strings under its own random prefix. No caller retry or server setting
change is performed.

## Completed independent controls

- RedisPy 8.1.0 raw held same/different physical connections reproduced the
  host-path delay in all 224 validated sequences without Go or the driver.
- The identical Go source on a Linux/arm64 client in the server network
  namespace passed all 448 sequences in 0.36 seconds. First PING medians were
  approximately 0.017 ms and every first observation was below 0.1 ms, including
  the larger payloads. Server settings and verified TLS/CA were unchanged.
- That comparison changed both client OS and route. It narrows the observation
  to the actual host/topology combination; it does not identify which forwarding,
  OS, TLS, proxy or scheduling mechanism caused it.

## Useful next controls

- Separate client OS from the host published-port versus direct namespace route
  while retaining the same expected TLS server identity and trusted certificate.
- Obtain packet/server-duration evidence before assigning a protocol mechanism.
- Keep the small/large GET and same/cross-client controls matched; changing only
  the production SQL planner cannot explain the standalone reproduction.
