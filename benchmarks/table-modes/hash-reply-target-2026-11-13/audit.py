"""Audit every retained trial and recompute the published comparison."""
import itertools
import json
from pathlib import Path
import re
import statistics

p = Path(__file__).with_name('results.json')
s = json.loads(p.read_text())
assert s['target_bytes'] == 16 << 20 and not s['target_is_hard_bound']
assert s['execution_order'] == ['baseline', 'candidate']
assert s['automatic_retries'] == s['attempts_discarded'] == 0
expected = set(itertools.product(('baseline', 'candidate'), (1, 4), (512, 4096), range(-1, 6)))
actual = set()
for r in s['samples']:
    identity = r['variant'], r['readers'], r['batch_rows'], r['repeat']
    assert identity not in actual
    actual.add(identity)
    assert r['validated'] and r['warmup'] == (r['repeat'] == -1)
    assert r['rows'] == 100000 and r['columns'] == 105
    assert r['tasks'] == (1 if r['readers'] == 1 else 8)
    assert r['seconds'] > 0 and r['first_batch_seconds'] >= 0 and r['peak_worker_rss_bytes'] > 0
assert actual == expected and len(s['samples']) == 56
assert len(s['summary']) == 8
for row in s['summary']:
    group = [r for r in s['samples'] if not r['warmup'] and all(
        r[key] == row[key] for key in ('variant', 'readers', 'batch_rows'))]
    assert row['measured_samples'] == len(group) == 6
    for field in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
        vals = [r[field] for r in group]
        assert row[field] == {'median': statistics.median(vals), 'min': min(vals), 'max': max(vals)}
for build in s['builds']:
    assert build['exit_code'] == 0
    for field in ('driver_sha256', 'cli_sha256'):
        assert re.fullmatch('[0-9a-f]{64}', build[field])
for value in [s['source_sha256'], *s['candidate_source_sha256'].values()]:
    assert re.fullmatch('[0-9a-f]{64}', value)
print('Audited all 56 trials, eight six-sample distributions and build/source hash formats')
