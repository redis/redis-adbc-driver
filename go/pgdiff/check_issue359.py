"""Require an observed PostgreSQL 18 pass for every retained #359 baseline case."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def check(report, inventory):
    if not report.get('reference', {}).get('postgres', '').startswith('PostgreSQL 18.'):
        raise ValueError('a live PostgreSQL 18 report is required')
    if report.get('infrastructure_errors'):
        raise ValueError('report contains infrastructure errors')
    expected = {(r['file'], r['id']) for r in inventory['cases']}
    if len(expected) != len(inventory['cases']) or len(expected) != 188:
        raise ValueError('the baseline must contain 188 unique cases')
    actual = {}
    for row in report['results']:
        key = row['file'], row['id']
        if key in actual:
            raise ValueError(f'duplicate report case: {key}')
        actual[key] = row['status']
    remaining = [(file, case, actual.get((file, case), 'missing'))
                 for file, case in sorted(expected)
                 if actual.get((file, case)) != 'pass']
    return remaining


def audit(report, inventory):
    """Check triage coverage separately; a disposition never changes a test result."""
    remaining = check(report, inventory)
    rows = {(r['file'], r['id']): r for r in inventory['cases']}
    categories = {'Core-related', 'optional standard', 'PostgreSQL-specific',
                  'ADBC representation/comparison', 'fixture/oracle'}
    for key, row in rows.items():
        if row.get('classification') not in categories or not row.get('classification_note'):
            raise ValueError(f'unclassified case: {key}')
        if not row.get('root_cause') or not row.get('evidence'):
            raise ValueError(f'missing root cause/evidence: {key}')
    for file, case, status in remaining:
        row = rows[file, case]
        if status != 'fail':
            raise ValueError(f'unobserved failing case: {file}:{case}: {status}')
        if row.get('disposition') != 'deferred' or not row.get('reason'):
            raise ValueError(f'undisposed failure: {file}:{case}')
        if not row.get('owner', '').startswith('https://github.com/redis/redis-adbc-driver/issues/'):
            raise ValueError(f'missing owning issue: {file}:{case}')
    return remaining


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('report', type=Path)
    p.add_argument('--inventory', type=Path, default=ROOT / 'issue359-cases.json')
    p.add_argument('--triage', action='store_true', help='validate documented dispositions, without claiming passes')
    args = p.parse_args()
    try:
        verify = audit if args.triage else check
        remaining = verify(json.loads(args.report.read_text()), json.loads(args.inventory.read_text()))
    except (ValueError, KeyError) as exc:
        p.error(str(exc))
    print(f'{188-len(remaining)}/188 retained cases passed; {len(remaining)} unresolved')
    for file, case, status in remaining:
        print(f'{file}:{case}: {status}')
    if args.triage:
        print('188/188 triaged; deferred failures remain failures. This is not a conformance pass.')
        return 0
    return int(bool(remaining))


if __name__ == '__main__':
    raise SystemExit(main())
