"""Select normal native managed writes and redis-py controls from the existing harness.

No driver monkeypatches or statement instrumentation; full original oracles remain.
"""
from pathlib import Path
import hashlib
import os
import sys

# An explicit root permits freezing this same wrapper outside either source tree.
root = Path(os.environ.get('REDIS_WRITER_HARNESS_ROOT', Path(__file__).resolve().parents[2]))
sys.path.insert(0, str(root/'benchmarks/table-modes'))
import scenarios

enabled_text = os.environ.get('REDIS_WRITER_QUEUE', 'false')
if enabled_text not in ('true', 'false'):
    raise ValueError('REDIS_WRITER_QUEUE must be true or false')
queue_enabled = enabled_text == 'true'
option = 'adbc.redis.writer_admission_queue'
original_connect = scenarios.base.connect
def connect(cfg):
    if not queue_enabled:
        return original_connect(cfg)  # Baseline accepts no new/unknown option.
    connection = scenarios.base.adbc.connect(driver=cfg['driver'], autocommit=True,
        db_kwargs={'uri': cfg['uri'], 'adbc.redis.stream_batch_rows': '4096', option: 'true'})
    if connection.adbc_database.get_option(option) != 'true':
        connection.close()
        raise RuntimeError('Driver did not report the requested admission option')
    return connection
scenarios.base.connect = connect
scenarios.POINT = ('insert', 'update', 'delete')
scenarios.base.MODES = ('managed', 'redis_py')
original_init = scenarios.ScenarioBenchmark.__init__
def initialize(self, args):
    if args.suite != 'oltp':
        raise ValueError('This confirmation selects only the OLTP write suite')
    original_init(self, args)
    self.results['metadata'].update(selection_wrapper_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        selected_operations=list(scenarios.POINT), selected_modes=list(scenarios.base.MODES),
        writer_admission_queue_requested=queue_enabled, writer_admission_queue_verified=queue_enabled,
        selection_scope='Uninstrumented normal Python/ADBC managed writes vs redis-py; persistent per-thread clients, independent database handles in one process;1/4 clients; all original result and full final-state oracles; no global/cross-process FIFO claim.')
    self.save()
scenarios.ScenarioBenchmark.__init__ = initialize
scenarios.main()
