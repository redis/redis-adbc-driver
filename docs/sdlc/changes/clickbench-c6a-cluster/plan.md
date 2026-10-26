# Plan and evidence

1. Verify AWS profile, instance availability, subnet and source revision.
2. Provision six dedicated tagged instances and automatic termination timers.
3. Add explicit self-hosted mode and cluster-wide indexing readiness.
4. Build the driver natively, run harness checks and verify all primaries.
5. Run a typed official-source pilot and measure per-node memory. Validate
   completed query outputs, preserve artifacts, then decide whether full scale
   fits the actual resource budget.
6. Terminate only this run's instances, remove its security group and key,
   and verify cleanup. Retain outcomes and limitations in the benchmark report.

Pilot complete: all six primaries healthy; all 16,384 slots covered. Native
Linux driver build passed. 67 default helper/unit checks passed; three default
skips were then run successfully as real Linux cgroup checks. All 43 queries
completed three trials. The standard oracle validated 126 trials; the three Q19
ties passed supplemental independent full-group/rank validation. Full scale was
not attempted: 0.875 GiB used at 100,000 rows projects to about 875 GiB, exceeding
the six nodes' 192 GiB host RAM. This is an estimate, not an observed full-scale
OOM. Results and per-query timings are in
[the report](../../../../benchmarks/clickbench/c6a-cluster-2026-10-21/report.md).

Artifacts were copied locally before requesting termination of all six nodes.
Cleanup verification is recorded with the benchmark evidence.
Operational state is outside git in a protected temporary directory. The
generated private SSH key was removed locally after instance termination and
AWS key-pair deletion. No credentials are included in repository artifacts.
See [intent](intent.md) and [spec](spec.md).
