#!/usr/bin/env python3
"""Archive an exact commit and build a private, instrumented native library.

No shipping tree is modified. Run on Linux before the diagnostic, never during
performance windows. The probe supports a single Software proxy endpoint only.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError('Archived source does not match the reviewed probe insertion')
    return text.replace(old, new)


def main():
    if sys.version_info < (3, 14):
        raise SystemExit('Use Python 3.14 or newer for native probe builds')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--race', action='store_true')
    parser.add_argument('--checkptr', action='store_true')
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    work = args.work.resolve()
    if work.exists() or not work.parent.is_dir():
        raise ValueError('Use a new private directory with an existing parent')
    if subprocess.run(['git', '-C', str(work.parent), 'rev-parse', '--show-toplevel'],
                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise ValueError('Private build must be outside a Git checkout')
    base = subprocess.check_output(['git', 'rev-parse', '--verify', args.base+'^{commit}'],
                                   cwd=repo, text=True).strip()
    archive = subprocess.check_output(['git', 'archive', base, 'go'], cwd=repo)
    work.mkdir(mode=0o700)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(work, filter='data')
    tree = work / 'go'
    fixture = Path(__file__).with_name('native_proofs.go.txt').read_text()
    # Reuse the exact source revision's existing argument-redacting categories.
    # Narrow declaration boundaries fail closed when those helpers change.
    helpers = []
    for filename, start, end in (
        ('snapshot_control_script_test.go', 'func snapshotObservedScriptBody(', '\nfunc TestSnapshotControlScriptObservation'),
        ('snapshot_compaction_profile_test.go', 'func compactionProfileCategory(', '\nfunc (p *compactionPhaseProfile) current('),
        ('crud_execution_profile_test.go', 'func crudProfileCategory(', '\n// Counts payload values only:'),
    ):
        source = (tree / filename).read_text()
        if source.count(start) != 1 or source.count(end) != 1:
            raise ValueError('Cannot extract reviewed diagnostic categorizer')
        helpers.append(source.split(start)[1].split(end)[0])
        helpers[-1] = start + helpers[-1]
    fixture = replace_once(fixture, '\t"encoding/json"',
                           '\t"encoding/json"\n\t"fmt"\n\t"strconv"\n\t"strings"')
    fixture += '\n' + '\n'.join(helpers)
    (tree / 'native_proofs.go').write_text(fixture)
    bridge = Path(__file__).with_name('native_proofs_bridge.go.txt').read_bytes()
    (tree / 'pkg/native_proofs.go').write_bytes(bridge)
    timeout = tree / 'timeout.go'
    signature = 'func newClient(cfg *clientConfig, cluster bool, t timeouts) goredis.UniversalClient {\n'
    timeout.write_text(replace_once(timeout.read_text(), signature,
        signature.replace(') goredis.UniversalClient', ') (result goredis.UniversalClient)') +
        '\tif cluster { panic("native proof probe requires a single Software proxy endpoint") }\n'
        '\tdefer func() { result.AddHook(newNativeProofHook()) }()\n'))
    statement = tree / 'statement.go'
    signature = 'func (s *statementImpl) ExecuteQuery(ctx context.Context) (reader array.RecordReader, count int64, resultErr error) {\n'
    statement.write_text(replace_once(statement.read_text(), signature,
                                      signature + '\tctx = nativeProofContext(ctx)\n'))
    env = os.environ.copy()
    for key in ('GOFLAGS', 'GOWORK', 'GOMAXPROCS', 'GOTOOLCHAIN', 'GOEXPERIMENT',
                'CGO_CFLAGS', 'CGO_CXXFLAGS', 'CGO_LDFLAGS', 'REDIS_URI', 'REDIS_SNAPSHOT_URI'):
        env.pop(key, None)
    env.update(GOWORK='off', CGO_ENABLED='1')
    goos = subprocess.check_output(['go', 'env', 'GOOS'], cwd=tree, env=env, text=True).strip()
    if goos != 'linux':
        raise ValueError('Build this native attribution probe on Linux')
    files = {str(p.relative_to(tree)): sha(p.read_bytes()) for p in (
        tree/'go.mod', tree/'go.sum', timeout, statement, tree/'native_proofs.go', tree/'pkg/native_proofs.go')}
    manifest = {'source_commit': base, 'archive_sha256': sha(archive), 'overlaid_files': files,
                'builder_sha256': sha(Path(__file__).read_bytes()), 'race': args.race, 'checkptr': args.checkptr,
                'go_version': subprocess.check_output(['go', 'version'], cwd=tree, env=env, text=True).strip()}
    command = ['go', 'build', '-buildmode=c-shared', '-tags', 'driverlib,nativeproofprobe',
               '-ldflags', '-X github.com/adbc-drivers/driverbase-go/driverbase.infoDriverVersion=v0.1.0-dev.native-proof-diagnostic',
               '-o', str(work/'probe.so'), './pkg']
    if args.race:
        command.insert(2, '-race')
    if args.checkptr:
        command.insert(2, '-gcflags=all=-d=checkptr=2')
    with (work/'build.log').open('w') as log:
        result = subprocess.run(command, cwd=tree, env=env, stdout=log, stderr=subprocess.STDOUT)
    manifest.update(build_exit_code=result.returncode, build_log_sha256=sha((work/'build.log').read_bytes()))
    if result.returncode == 0:
        manifest.update(library_sha256=sha((work/'probe.so').read_bytes()),
                        build_info=subprocess.check_output(['go', 'version', '-m', str(work/'probe.so')], cwd=tree, env=env, text=True))
    (work/'manifest-private.json').write_text(json.dumps(manifest, indent=2)+'\n')
    if result.returncode:
        raise RuntimeError('Private build failed; retain its log and manifest')
    print('Private native proof diagnostic built; shipping sources unchanged.')


if __name__ == '__main__':
    main()
