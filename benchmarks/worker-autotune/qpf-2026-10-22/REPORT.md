# Controlled autotuning evaluation

Gate status: **cancelled**

Superseded by the requested Standard/2x/4x/8x comparison with equal server CPU limits. Partial evidence only.

Cold bounds: QPS >= 85% and p95 <= 120% of the CPU-sized baseline. Warmed bounds: QPS >= 90% and p95 <= 115% of the highest-throughput measured fixed policy.

| Deployment / rows / CPU / link / arrivals / concurrency | Fixed reference | Warm QPS ratio | Warm p95 ratio | Cold QPS ratio | Cold p95 ratio | Pass |
|---|---|---:|---:|---:|---:|---|

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'continuous', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'continuous', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'continuous', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}
