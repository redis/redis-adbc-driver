"""Reserved-window supervisor; configuration contains paths, never credentials."""
import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
import time

config = json.loads(Path(sys.argv[1]).read_text())
runner = Path(config['runner']).resolve()
spec = importlib.util.spec_from_file_location('aggregate406', runner)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
control = Path(config['control_output'])
uri = None

def interrupted(signum, frame):
    raise KeyboardInterrupt('supervisor signal ' + str(signum))

signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGINT, interrupted)
try:
    gate = json.loads(Path(config['gate']).read_text())
    module.check_full_window(gate, config['schema'], config['deadline'], time.time())
    if config['supervisor_unit'] not in gate.get('active_job_units', []):
        raise RuntimeError('supervisor ownership must be journaled before launch')
    credentials = json.loads(Path(config['credential_file']).read_text())
    uri = credentials['uri']
    credentials.clear()
    os.environ['REDIS_URI'] = uri
    sys.argv = [str(runner), '--baseline', config['baseline'], '--candidate', config['candidate'],
                '--parquet', config['parquet'], '--work', config['work'], '--schema', config['schema'],
                '--provenance', config['provenance'], '--rows', '99997497', '--workers', '16',
                '--repeats', '3', '--timeout', '1200', '--memory-mib', '16384',
                '--full-gate', config['gate'], '--deadline', str(config['deadline'])]
    module.save(control, {'status': 'starting', 'supervisor_unit': config['supervisor_unit'],
                          'deadline': config['deadline'], 'started_at': time.time()})
    module.main()
    if config.get('profiles'):
        profile_spec=importlib.util.spec_from_file_location('full_profiles',config['profile_controller'])
        profile_module=importlib.util.module_from_spec(profile_spec);profile_spec.loader.exec_module(profile_module)
        profile_module.run_profiles(config,uri,module)
    module.save(control, {'status': 'complete', 'supervisor_unit': config['supervisor_unit'],
                          'finished_at': time.time()})
except BaseException as error:
    details = module.error_details(error, uri)
    module.save(control, {'status': 'failed', 'supervisor_unit': config['supervisor_unit'],
                          'finished_at': time.time(), 'failure': details})
    print(json.dumps({'supervisor_failed': details}), flush=True)
    sys.exit(1)
finally:
    os.environ.pop('REDIS_URI', None)
