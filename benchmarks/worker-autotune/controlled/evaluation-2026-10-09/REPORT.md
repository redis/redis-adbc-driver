# Controlled autotuning evaluation

Gate status: **cancelled**

Stopped by user request after 47 of 222 invocations (141 of 666 records). Missing repetitions are incomplete coverage, not measured Redis failures. No complete three-repetition comparison exists; readiness is unproven. Medians and full min/max spread across independent repeated trials; these are not confidence intervals. Warmed windows can still include probes. Local allocation is not a global optimum guarantee.

Cold bounds: QPS >= 85% and p95 <= 120% of the CPU-sized baseline. Warmed bounds: QPS >= 90% and p95 <= 115% of the highest-throughput measured fixed policy.

| Deployment / rows / CPU / link / arrivals / concurrency | Fixed reference | Warm QPS ratio | Warm p95 ratio | Cold QPS ratio | Cold p95 ratio | Pass |
|---|---|---:|---:|---:|---:|---|

Incomplete coverage: {'case': ('oss', 32768, 1, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 1, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 1, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 1, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 1, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 1, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'server-transition', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'server-transition', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'server-transition', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 2, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 32768, 4, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'continuous', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'continuous', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'continuous', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('oss', 100000, 2, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 1, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'server-transition', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'server-transition', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'server-transition', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 2, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'aggregate-response-2ms', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'aggregate-response-2ms', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'aggregate-response-2ms', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 32768, 4, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'continuous', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'continuous', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'continuous', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'waves', 1), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'waves', 2), 'error': 'missing, duplicate, or unexpected policy/repetition'}

Incomplete coverage: {'case': ('software', 100000, 2, 'direct', 'waves', 4), 'error': 'missing, duplicate, or unexpected policy/repetition'}
