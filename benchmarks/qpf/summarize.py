"""Produce a Markdown report from run.py's raw trial results."""
import collections
import json
import pathlib
import statistics

root = pathlib.Path(__file__).parent
data = json.loads((root / 'results.json').read_text())
groups = collections.defaultdict(list)
for trial in data['trials']:
    groups[trial['concurrency'], trial['query'], trial['qpf']].append(trial)

lines = [
    '# Local QPF benchmark', '',
    'Measured on October 6, 2026 using a dedicated disposable Redis Software container.', '',
    '## Method', '',
    '- Redis Software image: `redislabs/redis:8.2.0-78.18`; database Redis 8.6.2, Search 8.6.11.',
    '- One shard, no replica, RAM storage, 1 GiB database limit, 100,000 deterministic hashes with a 512-byte payload.',
    '- Docker host: 10 virtual CPUs, approximately 7.75 GiB RAM, ARM64.',
    '- Actual QPF API settings: disabled/standard, 2×, and 4×; API readback verified after every update.',
    '- Six direct Redis command workloads, concurrency 1 and 16, three 3-second trials per combination.',
    '- QPF order rotated across rounds: standard/2×/4×, 4×/standard/2×, 2×/4×/standard.',
    '- Each worker has a separate connection, warms up before a synchronized start, and sends one request at a time.',
    '- Throughput counts completed requests over actual elapsed time, including in-flight requests finishing after the deadline.',
    '- Identical query results checked before each configuration; both aggregation variants produce the same sums.',
    '- Tables show median trial throughput and median trial p95; p95 is not pooled across trials.', '',
    '## Limits', '',
    'This is a shared local Docker host, not a Redis Cloud deployment or an isolated capacity test. '
    'During execution another Redis test container consumed about 337% CPU while this container consumed about 518%. '
    'Background load varied; throughput ranges below expose run-to-run variability. '
    'Do not interpret differences as a pure causal estimate of QPF or guaranteed production speedups. '
    'Some slow aggregation trials complete few requests, so their p95 estimates are coarse. '
    'Python client parsing and thread scheduling can also limit large-result throughput. '
    'This measures direct FT commands, not end-to-end ADBC SQL/Arrow performance. '
    'No cloud or existing application databases were changed.', '',
]
for concurrency in [1, 16]:
    lines += [f'## Concurrency {concurrency}', '',
              '| Workload | QPF | Median QPS | QPS range | Median p95 ms | QPS vs standard | Trials |',
              '|---|---:|---:|---:|---:|---:|---:|']
    for name in data['metadata']['queries']:
        baseline = statistics.median(r['qps'] for r in groups[concurrency, name, 0])
        for factor in [0, 2, 4]:
            rows = groups[concurrency, name, factor]
            if not rows:
                continue
            qps = [r['qps'] for r in rows]
            median = statistics.median(qps)
            p95 = statistics.median(r['p95_ms'] for r in rows)
            lines.append(f'| {name} | {factor or "standard"} | {median:,.1f} | '
                         f'{min(qps):,.1f}–{max(qps):,.1f} | {p95:.2f} | {median / baseline:.2f}× | {len(rows)} |')
    lines.append('')
lines += ['## Exact queries', '']
for name, query in data['metadata']['queries'].items():
    lines += [f'### {name}', '', '```text', ' '.join(str(x) for x in query), '```', '']
lines += ['## Validation', '',
          f"Completed trials: {len(data['trials'])}/108. Total command errors: {sum(t['errors'] for t in data['trials'])}.", '',
          'Raw records, versions, effective configurations, and correctness hashes: `results.json`.', '',
          'Reproduce against a new dedicated container:', '', '```sh',
          'docker run -d --name redis-adbc-qpf-bench --cap-add SYS_RESOURCE \\',
          '  -p 127.0.0.1:19443:9443 -p 127.0.0.1:12100:12100 redislabs/redis:8.2.0-78.18',
          'uv run --with redis python benchmarks/qpf/run.py',
          'python3 benchmarks/qpf/summarize.py',
          'docker rm -f redis-adbc-qpf-bench', '```', '']
(root / 'report.md').write_text('\n'.join(lines))
print('\n'.join(lines[:48]))
