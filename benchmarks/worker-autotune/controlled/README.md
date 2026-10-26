# Controlled worker autotuning gate

This runner records its objective, thresholds, randomized schedule, binary hash,
source revision and diff hash, image digests/architectures, server configuration,
client limits, and surrounding Docker activity in `manifest.json` **before** the
first trial. Each policy/repetition runs in a fresh native Linux client container
with a real 1/2/4 CPU quota, matching `GOMAXPROCS`, and a 2 GiB memory limit.
Use disposable, otherwise idle Redis deployments with Search enabled. The fixture
is `it_worker_profile_<rows>`; fixture reuse checks the sequence and indexed count.
Every measured query is checked against an independent serial result.

The full default matrix has OSS and Redis Software, 32,768 rows, CPU 1/2/4,
concurrency 1/2/4, direct and delayed aggregate responses, four policies, and
three independent repetitions. Additional 100,000-row checks use CPU 2, direct
links, and both synchronized waves and continuously cycling clients. These
additional checks are spot checks, not a second full factorial. Each trial has
60 learning rounds followed by 18 measured rounds; the latter can still contain
probes, so inspect `allocation_history` before calling an allocation converged.
Concurrency changes within a trial exercise cached workload/concurrency models.
Continuous clients independently issue their next query without a wave barrier;
the learning-to-measurement boundary drains outstanding queries. Additional
32,768-row server-transition checks keep the client at CPU 2, learn against
a 2-CPU server, then change the isolated server to 1 CPU at each measurement
boundary using acknowledged file handshakes. The quota returns to 2 before
the next learning phase and at trial cleanup. These checks test recovery after
a controlled Redis CPU-capacity change; they are not stationary steady state.

Policies are serial (per-query 1/shared CPU), CPU fixed (CPU/CPU), headroom fixed
(2*CPU/2*CPU), and tuned (auto/auto). Policy and scenario order is independently
shuffled in each repetition with the archived seed. Cold QPS must be at least
85% and cold p95 at most 120% of CPU fixed. Warmed QPS must be at least 90% and
p95 at most 115% of the **same highest-throughput fixed policy**, selected using
medians across repetitions. Serial participates in fixed reference selection.
The report preserves min/max trial spread; three repetitions do not support a
strong statistical confidence claim. These are readiness gates, not claims of
a global optimum or guarantees for untested workloads.

The delayed condition waits 2 ms once after each complete `FT.AGGREGATE` or
`FT.CURSOR` response. It preserves RESP framing and pipelining; it does not delay
TCP chunks, setup commands, or all traffic and is **not network RTT**. The native Go
proxy shares the client quota and adds parsing/CPU overhead. The condition models
this specific command-response path, not a general network deployment. Proxy
counts/errors and cgroup state are archived. It is separately unit tested for
segmented/coalesced replies and pipeline delay accounting.

Prerequisites: Docker with a private network, native `python:3.14-slim`, a native
static Go test binary, and two isolated servers. Example (from repo root):

```sh
(cd go && CGO_ENABLED=0 go test -c -o build/autotune-profile-linux . && \
  CGO_ENABLED=0 go build -o build/autotune-proxy-linux ./internal/benchproxy)
python3 -m unittest discover -s benchmarks/worker-autotune/controlled -v
python3 benchmarks/worker-autotune/controlled/run.py \
  --binary go/build/autotune-profile-linux --proxy-binary go/build/autotune-proxy-linux --network YOUR_PRIVATE_NETWORK \
  --server oss=redis://oss:6379/0 --server software=redis://software:12000/0 \
  --server-container YOUR_OSS_CONTAINER --server-container YOUR_SOFTWARE_CONTAINER \
  --output /tmp/autotune-controlled-new
```

The output path must be new so a run cannot silently replace its predeclared
manifest. Exit status is 0 for a passing gate, 2 for measured performance
regressions, and 1 for infrastructure failure or incomplete/duplicate trials.
An operator interrupt exits 130, cleans up the current trial, and records a
`cancelled` report with partial evidence; missing coverage is not a measured
Redis failure.
All cases run even if a gate fails. Archive `manifest.json`, `results.jsonl.gz`,
`gate.json`, `REPORT.md`, `completion.json`, and logs on either success or failure.
Results include per-query latency, cold learning, CPU, heap, allocations,
process-lifetime RSS, Redis CPU/memory/network/command counters, grants and model
snapshots. Redis INFO measures the whole isolated server; client CPU measures
the Go process, while RSS includes setup/warmup. The runner does not isolate the
Docker VM from other host processes; record and interpret residual host load.

The workflow `worker-autotune.yml` provides a manual archived run. A failed gate
must remain visible and feed the optimization issue rather than be relabelled
as successful validation. Default readiness also requires production-like
server-load changes and broader workload validation beyond this finite matrix.

To diagnose the older policy, build the base revision with the same
`worker_budget_profile_test.go`, then pass `--baseline-binary PATH`. The runner
randomizes a fifth `legacy-tuned` policy into OSS/CPU4 blocks and records its
binary hash. It is diagnostic and does not participate in fixed-reference gate
selection. This compares policies under identical fixture, quotas and profiler;
it does not retroactively control the original workstation experiment.

The [stopped October 9 evaluation](evaluation-2026-10-09/PROBLEMS.md) preserves
47 of 222 invocations and documents provisional regressions. It does not pass
the readiness gate or validate the final recovery guard through performance trials.
