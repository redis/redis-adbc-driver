"""Hard memory boundaries for Linux ClickBench trial processes.

Transient services are siblings of the supervisor, not children of its cgroup.
The supervisor survives a trial's OOM kill and persists the failed trial.
"""
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import uuid

MIB = 1024 * 1024


def memory_capacity(proc=Path('/proc'), cgroup=Path('/sys/fs/cgroup')):
    """Available memory, accounting for the current cgroup and its ancestors."""
    info = {}
    for line in (proc / 'meminfo').read_text().splitlines():
        key, value = line.split(':', 1)
        info[key] = int(value.split()[0]) * 1024
    capacity = min(info['MemTotal'], info['MemAvailable'])
    membership = next((line.split(':', 2)[2] for line in
                       (proc / 'self/cgroup').read_text().splitlines()
                       if line.startswith('0::')), None)
    if membership is None or not (cgroup / 'cgroup.controllers').exists():
        raise RuntimeError('Trial isolation requires Linux cgroup v2 memory control')
    directory = cgroup / membership.lstrip('/')
    for parent in [directory, *directory.parents]:
        if parent != cgroup and cgroup not in parent.parents:
            break
        maximum = parent / 'memory.max'
        current = parent / 'memory.current'
        if maximum.exists() and maximum.read_text().strip() != 'max':
            capacity = min(capacity, max(0, int(maximum.read_text()) - int(current.read_text())))
    return capacity


def resolve_memory_limit(requested_mib=None):
    capacity = memory_capacity()
    # Reserve half for the supervisor, kernel, credentials and other services.
    limit = capacity // (2 * MIB) if requested_mib is None else requested_mib
    if limit < 128 or limit * MIB > capacity * 3 // 4:
        raise ValueError('Trial memory must be at least 128 MiB and leave 25% of available RAM free')
    return limit


def _control(*args, check=False):
    return subprocess.run(['systemctl', '--no-ask-password', *args],
                          capture_output=True, text=True, timeout=30, check=check)


def run_isolated(command, timeout, memory_mib, *, credentials=None):
    """Return safe failure metadata; never return subprocess logs or credentials."""
    if timeout <= 0 or not math.isfinite(timeout) or memory_mib <= 0:
        raise ValueError('A finite positive timeout and memory limit are required')
    unit = 'adbc-clickbench-trial-' + uuid.uuid4().hex + '.service'
    launch = ['systemd-run', '--no-ask-password', '--quiet', '--wait', '--pipe',
              '--service-type=exec', '--unit=' + unit,
              '--property=MemoryAccounting=yes',
              f'--property=MemoryMax={memory_mib * MIB}',
              '--property=MemorySwapMax=0', '--property=OOMPolicy=stop',
              '--property=KillMode=control-group', '--property=TimeoutStopSec=5s',
              f'--property=RuntimeMaxSec={timeout}s',
              '--', *map(str, command)]
    # Credentials cross stdin only, never command arguments, job files or logs.
    secret = json.dumps(credentials).encode() if credentials is not None else b''
    record = {'trial_memory_limit_mib': memory_mib, 'trial_isolation': 'systemd-cgroup-v2'}
    try:
        try:
            process = subprocess.run(launch, input=secret, capture_output=True,
                                     timeout=timeout + 15)
            record['exit_code'] = process.returncode
        except subprocess.TimeoutExpired:
            record.update(error_type='QueryDeadline', seconds=None)
        properties = _control('show', unit, '--property=Result', '--property=MemoryPeak')
        values = dict(line.split('=', 1) for line in properties.stdout.splitlines() if '=' in line)
        peak = values.get('MemoryPeak', '')
        if peak.isdigit() and int(peak) < (1 << 64) - 1:
            record['trial_memory_peak_bytes'] = int(peak)
        result = values.get('Result')
        if result == 'oom-kill':
            record.update(seconds=None, error_type='MemoryLimit')
        elif result == 'timeout':
            record.update(seconds=None, error_type='QueryDeadline')
        elif record.get('exit_code', 0) != 0 and 'error_type' not in record:
            record.update(seconds=None, error_type='WorkerExit')
        return record
    finally:
        # Killing systemd-run alone would orphan the trial; stop the entire unit.
        _control('stop', unit)
        _control('reset-failed', unit)


def check_isolation(memory_mib):
    """Fail before ingestion if the requested memory boundary cannot be used."""
    if not shutil.which('systemd-run') or not shutil.which('systemctl'):
        raise RuntimeError('ClickBench trial isolation requires Linux systemd-run and systemctl')
    controllers = Path('/sys/fs/cgroup/cgroup.controllers').read_text().split()
    if 'memory' not in controllers:
        raise RuntimeError('The cgroup v2 memory controller is unavailable')
    record = run_isolated(['/bin/true'], 10, memory_mib)
    if record.get('error_type') or record.get('exit_code') != 0:
        raise RuntimeError('Cannot create memory-isolated trials; run with system-manager permission')
