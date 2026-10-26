"""Identify an orphaned scan without relaunching or changing its timing."""
from pathlib import Path


def identify_job(job_path, job, report, root):
    root = Path(root)
    path = Path(job_path)
    if path.parent != root / 'results/scan-paths-fixed-jobs':
        raise ValueError('Unexpected scan job directory')
    if job.get('driver') != str(root / 'fix-source/go/build/libadbc_driver_redis.so'):
        raise ValueError('Unexpected scan driver')
    if job.get('namespace') != report['namespace']:
        raise ValueError('Unexpected scan namespace')
    cases = [c for c in report['cases'] if c['sql'] == job.get('sql')]
    if len(cases) != 1 or job.get('api') not in ('statement', 'partitions'):
        raise ValueError('Unexpected scan case/API')
    name = path.name.removesuffix('-job.json')
    case = cases[0]['name']
    prefix = case + '-'
    if not name.startswith(prefix):
        raise ValueError('Job filename differs from SQL case')
    trial, api, *suffix = name[len(prefix):].split('-')
    if trial not in ('1', '2', '3') or api != job['api']:
        raise ValueError('Job filename differs from measurement identity')
    if job.get('output') != str(path.with_name(name + '-result.json')):
        raise ValueError('Unexpected scan output path')
    if job.get('data_directory') != str(root / 'scan-data/fixed' / name):
        raise ValueError('Unexpected raw Arrow directory')
    return {'case': case, 'trial': int(trial), 'api': api}


def adopted_record(identity, unit, properties, output):
    if properties.get('ActiveState') in ('active', 'activating', 'deactivating'):
        raise ValueError('Cannot adopt an active scan')
    record = {**identity, 'seconds': None, 'trial_unit': unit,
              'trial_timeout_seconds': 1800, 'adopted_after_supervisor_restart': True}
    if properties.get('Result') == 'success' and output is not None:
        record.update(output)
        # Worker output must never replace the verified job identity.
        record.update(identity)
    else:
        record['error_type'] = {'timeout': 'QueryDeadline', 'oom-kill': 'MemoryLimit'}.get(
            properties.get('Result'), 'WorkerExit')
    peak = properties.get('MemoryPeak', '')
    if peak.isdigit() and int(peak) < (1 << 64) - 1:
        record['trial_memory_peak_bytes'] = int(peak)
    return record
