#!/usr/bin/env python3
"""Summarize the complete worker matrix; preserve raw per-round evidence."""
import csv
import json
from pathlib import Path

folder = Path(__file__).resolve().parent
records = [json.loads(line) for line in (folder / 'results.jsonl').read_text().splitlines()]
expected = {(link, cpu, concurrency, policy) for link in ('direct', 'impaired') for cpu in (1, 2, 4) for concurrency in (1, 2, 4) for policy in ('serial', 'fixed', 'tuned')}
indexed = {(r['condition'], r['GOMAXPROCS'], r['concurrency'], r['policy']): r for r in records}
assert set(indexed) == expected and len(records) == len(expected), 'incomplete or duplicate matrix'
fields = ['condition', 'GOMAXPROCS', 'concurrency', 'policy', 'rows', 'rounds', 'worker_ceiling', 'worker_budget', 'mean_query_seconds', 'p95_query_seconds', 'queries_per_second', 'client_cpu_seconds', 'client_cpu_percent', 'client_peak_heap_bytes', 'client_allocated_bytes', 'redis_network_bytes_per_second', 'redis_commands_per_second', 'redis_cpu_seconds', 'redis_memory_bytes']
with (folder / 'metrics.csv').open('w') as output:
    writer = csv.DictWriter(output, fieldnames=fields, extrasaction='ignore', lineterminator='\n')
    writer.writeheader()
    writer.writerows(records)
lines = ['# Worker autotuning evaluation — October 9, 2026', '',
'The tuned policy adapts during normal execution, but this short matrix does not establish maximum performance or a universal regression bound. Cold learning, noisy measurements, and bounded probes sometimes underperform fixed settings. Treat these as local evaluation results, not a production performance guarantee. Significant observed regressions leave this implementation in draft; default performance readiness requires further tuning and controlled, longer validation.', '',
'Environment: macOS arm64, Apple M4, Go 1.26.6, isolated Docker Redis Open Source 8.6.2 with Search. The workstation and Docker runtime are shared; the Go CPU capacities constrain the client runtime, not the Redis server. Results are one ordered run without randomized repetition. Redis Software and production-scale datasets were not measured in this run.', '',
f"Workload: {records[0]['rows']:,} rows, {records[0]['rounds']} rounds per case, `SELECT SUM(v), AVG(f), MIN(v), MAX(v)` with aggregate pushdown disabled. Serial uses one worker per query with a shared budget equal to CPU capacity. Fixed uses twice CPU capacity for both the query ceiling and shared budget. Tuned leaves both settings automatic. Each database handle is reused across the 1/2/4 concurrency phases; workload/concurrency changes restart learning. The impaired link delays each forwarded server-read chunk by 2 ms; this is not precise RTT emulation.", '',
'The optimization objective and decision thresholds are defined in [worker-autotuning.md](../../docs/worker-autotuning.md). The table includes cold learning and all probes. The p95 columns are measured query percentiles; the tuner itself uses a smoothed cohort maximum as its conservative tail signal.', '',
'| Link | CPU | Queries | Serial q/s | Fixed q/s | Tuned q/s | Tuned vs best fixed/serial | Serial p95 ms | Fixed p95 ms | Tuned p95 ms | Final tuned budget | Budget changes |',
'|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
throughput_deltas, tail_deltas = [], []
for link in ('direct', 'impaired'):
    for cpu in (1, 2, 4):
        for concurrency in (1, 2, 4):
            s, f, t = [indexed[link, cpu, concurrency, p] for p in ('serial', 'fixed', 'tuned')]
            delta = 100 * (t['queries_per_second'] / max(s['queries_per_second'], f['queries_per_second']) - 1)
            tail = 100 * (t['p95_query_seconds'] / min(s['p95_query_seconds'], f['p95_query_seconds']) - 1)
            throughput_deltas.append(delta); tail_deltas.append(tail)
            history = t['allocation_history']
            changes = sum(a['budget'] != b['budget'] for a, b in zip(history, history[1:]))
            lines.append(f"| {link} | {cpu} | {concurrency} | {s['queries_per_second']:.2f} | {f['queries_per_second']:.2f} | {t['queries_per_second']:.2f} | {delta:+.1f}% | {1000*s['p95_query_seconds']:.1f} | {1000*f['p95_query_seconds']:.1f} | {1000*t['p95_query_seconds']:.1f} | {t['worker_budget']} | {changes} |")
lines += ['', f"Across these cases, tuned throughput versus the better baseline ranges from {min(throughput_deltas):+.1f}% to {max(throughput_deltas):+.1f}%. Measured p95 relative to the better baseline ranges from {min(tail_deltas):+.1f}% to {max(tail_deltas):+.1f}%. The internal 10% throughput / 15% tail decision thresholds therefore do not imply equivalent bounds against independently chosen fixed configurations.", '',
'Convergence is workload dependent: the final budget can still be a probe. Budget changes count transitions across all 30 rounds, not just accepted improvements. Each candidate receives three comparable observations; four stable windows separate later probes. The raw allocation history contains every round, grant, signal, and reason, including regime resets and rollbacks. Mixed traffic is deliberately conservative. Longer runs and randomized repetitions across larger datasets and Redis deployments are needed before making broad performance claims.', '',
'[metrics.csv](metrics.csv) reports every case’s latency, throughput, client CPU seconds/utilization, peak Go heap, allocated bytes, network bytes/sec, Redis commands/sec, Redis CPU seconds, and Redis memory. Redis counters cover the entire isolated server; heap sampling is every 5 ms. Allocation snapshots and full convergence evidence are in [results.jsonl](results.jsonl). [overhead.txt](overhead.txt) reports the separate local telemetry/cohort microbenchmarks; these exclude SQL execution, so the profile comparisons provide the practical end-to-end cost.', '',
'Reproduce with:', '', '```sh', 'cd go', 'go test -c -o /tmp/redis-tuner.test .', 'cd ..', 'python3 benchmarks/worker-autotune/matrix.py --binary /tmp/redis-tuner.test --uri redis://127.0.0.1:16379/0 --rows 8192 --rounds 30 --output benchmarks/worker-autotune/results.jsonl', 'python3 benchmarks/worker-autotune/summarize.py', '```', '']
(folder / 'README.md').write_text('\n'.join(lines))
print(f'{len(records)} cases summarized; throughput delta {min(throughput_deltas):+.1f}%..{max(throughput_deltas):+.1f}%')
