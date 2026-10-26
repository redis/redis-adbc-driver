"""Read-only comparison of statement and public partition APIs on existing ClickBench data."""
import argparse
import datetime
import hashlib
import json
import math
import os
import re
from pathlib import Path
import sys
import time

import adbc_driver_manager.dbapi as dbapi

from isolation import check_isolation, run_isolated
from run import deployment_uri, save
from diagnostics import error_details
from scan_resume import check_resume, identity, replace_measurement, reusable

HERE = Path(__file__).resolve().parent


def finish_report(report):
    expected = {(case['name'], trial) for case in report['cases'] for trial in range(1, 4)}
    comparisons = report['comparisons']
    valid = {(c['case'], c['trial']) for c in comparisons if c.get('timings_eligible')}
    measurements = {(case, trial, api) for case, trial in expected for api in ('statement', 'partitions')}
    observed = {(t.get('case'), t.get('trial'), t.get('api')) for t in report['trials']}
    report['attempts_complete'] = len(report['trials']) == len(measurements) and observed == measurements
    report['valid_pairs'] = len(valid)
    report['complete'] = (report['attempts_complete'] and len(comparisons) == len(expected)
                          and valid == expected and all(
                              isinstance(t.get('seconds'), (int, float)) and math.isfinite(t['seconds'])
                              and t['seconds'] > 0 and not t.get('error_type')
                              for t in report['trials']))
    report['status'] = 'complete' if report['complete'] else 'failed'


def scan_cases(rows):
    bounded = min(1000000, rows)
    return [
        {'name': 'narrow_full', 'sql': 'SELECT __rowid, WatchID FROM hits',
         'source_columns': ['watchid'], 'source_where': '', 'expected_rows': rows, 'max_rowid': rows},
        {'name': 'filtered_full', 'sql': 'SELECT __rowid, WatchID, UserID, AdvEngineID FROM hits WHERE AdvEngineID <> 0',
         'source_columns': ['watchid', 'userid', 'advengineid'], 'source_where': 'WHERE AdvEngineID <> 0', 'expected_rows': None},
        {'name': 'mixed_bounded', 'sql': f'SELECT __rowid, WatchID, UserID, RegionID, URL, SearchPhrase, EventDate, EventTime FROM hits WHERE __rowid <= {bounded}',
         'source_columns': ['watchid', 'userid', 'regionid', 'url', 'searchphrase', 'eventdate', 'eventtime'],
         'source_where': None, 'expected_rows': bounded, 'max_rowid': bounded},
    ]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--prior-run', type=Path, required=True)
    ap.add_argument('--driver', type=Path, required=True)
    ap.add_argument('--parquet', type=Path, required=True)
    ap.add_argument('--upstream', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--data-directory', type=Path, required=True)
    ap.add_argument('--deadline', type=float, required=True)
    ap.add_argument('--skip-capability-probes', action='store_true',
                    help='Retest scan measurements only; do not repeat full-SQL capability probes')
    ap.add_argument('--resume', action='store_true', help='Preserve completed measurements and retry failures')
    ap.add_argument('--trial-timeout', type=float, default=1200,
                    help='Per-measurement cap in seconds, always bounded by --deadline')
    args = ap.parse_args()
    if not math.isfinite(args.trial_timeout) or args.trial_timeout <= 0:
        ap.error('Trial timeout must be finite and positive')
    prior = json.loads(args.prior_run.read_text())
    rows = prior['metadata']['loaded_rows']
    assert rows == prior['metadata']['requested_rows']
    with args.driver.open('rb') as stream:
        assert hashlib.file_digest(stream, 'sha256').hexdigest() == prior['metadata']['driver_binary_sha256']
    if not re.fullmatch(r'clickbench_[a-f0-9]{12}', prior['metadata']['namespace']):
        raise ValueError('Invalid existing benchmark namespace')
    uri = deployment_uri(prior['metadata']['deployment'], os.environ,
                         prior['metadata']['placement']['database'].get('primary_hosts', ()))
    check_isolation(16384)
    jobs = args.output.parent / (args.output.stem + '-jobs')
    jobs.mkdir(exist_ok=True)
    report = {'scope': 'Supplemental supported scan workload; not the 43-query ClickBench result',
              'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'loaded_rows': rows, 'namespace': prior['metadata']['namespace'],
              'driver_binary_sha256': prior['metadata']['driver_binary_sha256'],
              'placement': prior['metadata']['placement'], 'worker_budget': 16,
              'trial_memory_limit_mib': 16384, 'trials': [], 'comparisons': [],
              'cases': scan_cases(rows), 'native_clickbench_capabilities': [],
              'timer': 'Both paths include connection setup, full result consumption and Arrow IPC writes; native also includes descriptor planning and 16 worker process startup/teardown. Validation/hashes excluded.',
              'order': 'Pairs alternate statement/partitions then partitions/statement; three trials per API per case; no cold claim'}
    resume_tag = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    if args.resume:
        report = json.loads(args.output.read_text())
        check_resume(report, prior, scan_cases(rows))
        save(args.output.with_name(args.output.stem + '-before-resume-' + resume_tag + '.json'), report)
        report.setdefault('resumes', []).append({'at': resume_tag, 'trial_timeout_seconds': args.trial_timeout})
        report.update(complete=False, status='running')
    elif args.output.exists():
        ap.error('Output exists; use --resume to preserve previous attempts')
    report['trial_timeout_seconds'] = args.trial_timeout
    save(args.output, report)
    if args.resume:
        pass  # Capability probes are prior evidence, not repeated measurements.
    elif args.skip_capability_probes:
        report['capability_probes'] = 'not_repeated_in_targeted_failure_retest'
    else:
        # Actual public API calls, without fallback, document unsupported full SQL.
        with dbapi.connect(driver=str(args.driver), autocommit=True, db_kwargs={'uri': uri}) as conn:
            with conn.cursor() as cur:
                cur.execute('SET search_path TO ' + report['namespace'])
                for number, sql in enumerate(prior['queries'], 1):
                    try:
                        descriptors, _ = cur.adbc_execute_partitions(sql)
                        outcome = {'query': number, 'status': 'supported', 'descriptors': len(descriptors)}
                    except Exception as error:
                        outcome = {'query': number, 'status': 'unsupported' if type(error).__name__ == 'NotSupportedError' else 'error',
                                   **error_details(error, uri)}
                    report['native_clickbench_capabilities'].append(outcome)
    save(args.output, report)
    for case in report['cases']:
        for trial in range(1, 4):
            previous = next((c for c in report['comparisons']
                             if (c['case'], c['trial']) == (case['name'], trial)), None)
            existing = {(r['case'], r['trial'], r['api']): r for r in report['trials']}
            if previous and previous.get('timings_eligible') and all(
                    reusable(existing.get((case['name'], trial, api)), previous)
                    for api in ('statement', 'partitions')):
                continue
            pair = {}
            for api in (['statement', 'partitions'] if trial % 2 else ['partitions', 'statement']):
                old = existing.get((case['name'], trial, api))
                if reusable(old, previous):
                    record = dict(old)
                    record['seconds'] = old.get('seconds') or old['measured_seconds']
                    pair[api] = record
                    continue
                name = f'{case["name"]}-{trial}-{api}'
                if args.resume:
                    name += '-resume-' + resume_tag
                output = jobs / (name + '-result.json')
                job = {'api': api, 'driver': str(args.driver), 'namespace': report['namespace'],
                       'sql': case['sql'], 'output': str(output),
                       'data_directory': str(args.data_directory / name)}
                path = jobs / (name + '-job.json')
                save(path, job)
                record = {'case': case['name'], 'trial': trial, 'api': api, 'seconds': None,
                          'trial_timeout_seconds': args.trial_timeout}
                remaining = args.deadline - time.time()
                record['deadline_limited'] = remaining <= args.trial_timeout
                if remaining <= 0:
                    record['error_type'] = 'BudgetDeadline'
                else:
                    isolation = run_isolated([sys.executable, str(HERE / 'scan_trial.py'), str(path)],
                                             min(args.trial_timeout, remaining), 16384, credentials={'uri': uri})
                    record.update(isolation)
                    if not isolation.get('error_type') and output.exists():
                        record.update(json.loads(output.read_text()))
                    elif not isolation.get('error_type'):
                        record['error_type'] = 'MissingWorkerOutput'
                pair[api] = record
                replace_measurement(report, record)
                save(args.output, report)
                print(json.dumps({k: record.get(k) for k in ['case', 'trial', 'api', 'seconds', 'error_type', 'rows']}), flush=True)
            validation = {'case': case['name'], 'trial': trial, 'status': 'failed_trial', 'timings_eligible': False}
            if all(r.get('seconds') is not None for r in pair.values()) and args.deadline > time.time():
                name = case['name'] + '-' + str(trial)
                if args.resume:
                    name += '-resume-' + resume_tag
                output = jobs / (name + '-validation.json')
                job = {**pair, 'case': case, 'output': str(output), 'parquet': str(args.parquet),
                       'ddl': str(args.upstream / 'postgresql/create.sql'),
                       'temp_directory': str(args.data_directory / 'validation-spill')}
                path = jobs / (name + '-validation-job.json')
                save(path, job)
                isolated = run_isolated([sys.executable, str(HERE / 'scan_validate.py'), str(path)],
                                        min(900, args.deadline - time.time()), 16384)
                validation['deadline_limited'] = args.deadline - time.time() <= 0
                if not isolated.get('error_type') and output.exists():
                    validation.update(json.loads(output.read_text()))
                else:
                    validation.update(status='validation_error', error_type=isolated.get('error_type', 'MissingValidatorOutput'))
                validation['timings_eligible'] = validation['status'] == 'valid'
                if validation['timings_eligible']:
                    validation['statement_seconds'] = pair['statement']['seconds']
                    validation['partition_seconds'] = pair['partitions']['seconds']
                    validation['statement_over_partition_ratio'] = pair['statement']['seconds'] / pair['partitions']['seconds']
            if not validation['timings_eligible']:
                for record in pair.values():
                    record['measured_seconds'] = record.get('seconds')
                    record['seconds'] = None
            # Reused executions may have null eligible timings from a prior
            # failed partner. Only independent pair validation can restore them.
            for record in pair.values():
                for index, stored in enumerate(report['trials']):
                    if identity(stored) == identity(record):
                        report['trials'][index] = record
                        break
            if previous is not None:
                report.setdefault('prior_comparisons', []).append(previous)
                report['comparisons'].remove(previous)
            report['comparisons'].append(validation)
            save(args.output, report)
    report['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    finish_report(report)
    save(args.output, report)


if __name__ == '__main__':
    main()
