"""Retain successful measurements when resuming an interrupted scan comparison."""
import math


def identity(record):
    return record['case'], record['trial'], record['api']


def reusable(record, comparison):
    if not record or record.get('error_type'):
        return False
    if comparison and comparison.get('status') not in ('valid', 'failed_trial'):
        return False
    seconds = record.get('seconds', None)
    if seconds is None:
        seconds = record.get('measured_seconds')
    return (isinstance(seconds, (int, float)) and math.isfinite(seconds)
            and seconds > 0 and bool(record.get('files')))


def check_resume(report, prior, cases):
    for key in ('namespace', 'loaded_rows', 'driver_binary_sha256', 'placement'):
        if report.get(key) != prior['metadata'].get(key):
            raise ValueError('Resume ' + key + ' differs from recorded run')
    if report.get('cases') != cases:
        raise ValueError('Resume scan cases differ')
    expected = {(c['name'], t, api) for c in cases for t in (1, 2, 3)
                for api in ('statement', 'partitions')}
    identities = [identity(r) for r in report['trials']]
    if len(identities) != len(set(identities)) or not set(identities) <= expected:
        raise ValueError('Resume has duplicate or unexpected measurements')


def replace_measurement(report, record):
    old = next((r for r in report['trials'] if identity(r) == identity(record)), None)
    if old is not None:
        report.setdefault('prior_attempts', []).append(dict(old))
        report['trials'].remove(old)
    report['trials'].append(record)
