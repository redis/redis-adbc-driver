#!/usr/bin/env python3
"""Build both server-free probe variants in a new private directory.

This writes experimental binaries and a private provenance manifest only. It does
not run tests/timings, provision Redis, or change the shipping checkout.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile

BASE = '24f654b76b98f3d8960af94a3ca0230185d467a1'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--work', type=Path, required=True)
    p.add_argument('--race', action='store_true', help='Separate correctness binaries; never use for timing')
    args = p.parse_args()
    repo = Path(__file__).resolve().parents[2]
    work = args.work.resolve()
    assert not work.exists(), 'Use a new private directory; preserve earlier attempts'
    assert not subprocess.run(['git', '-C', str(work.parent), 'rev-parse', '--show-toplevel'],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 'Private work directory must be outside a Git checkout'
    work.mkdir()
    env = os.environ.copy()
    for key in ('GOFLAGS', 'GOWORK', 'GOMAXPROCS', 'REDIS_URI', 'REDIS_SNAPSHOT_URI'):
        env.pop(key, None)
    env.update(GOWORK='off', GOTOOLCHAIN='go1.27.3', CGO_ENABLED='1')
    archive = subprocess.check_output(['git', 'archive', BASE, 'go'], cwd=repo)
    source = Path(__file__).with_name('probe.go.txt')
    patch = Path(__file__).parent / 'patches/omit-forced-gc.patch'
    info = dict(base_commit=BASE, archive_sha256=hashlib.sha256(archive).hexdigest(),
                fixture_sha256=sha(source), patch_sha256=sha(patch), race=args.race,
                go=subprocess.check_output(['go', 'version'], env=env, text=True).strip(), builds={})
    for variant in ('baseline', 'candidate'):
        tree = work / (variant + '-source')
        tree.mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(tree, filter='data')
        (tree / 'go/pkg/ffi_release_probe.go').write_bytes(source.read_bytes())
        if variant == 'candidate':
            subprocess.run(['git', 'apply', '--check', str(patch)], cwd=tree, check=True)
            subprocess.run(['git', 'apply', str(patch)], cwd=tree, check=True)
        ext = {'darwin': 'dylib', 'linux': 'so', 'windows': 'dll'}[subprocess.check_output(
            ['go', 'env', 'GOOS'], env=env, text=True).strip()]
        binary = work / (variant + ('.race' if args.race else '') + '.' + ext)
        cmd = ['go', 'build', '-buildmode=c-shared', '-tags', 'driverlib,ffireleaseprobe',
               '-ldflags', '-s -w -X github.com/adbc-drivers/driverbase-go/driverbase.infoDriverVersion=v0.1.0-dev.ffi-release-gc',
               '-o', str(binary), './pkg']
        if args.race:
            cmd.insert(2, '-race')
        with (work / (variant + '-build.log')).open('w') as log:
            result = subprocess.run(cmd, cwd=tree / 'go', env=env, stdout=log, stderr=subprocess.STDOUT)
        info['builds'][variant] = dict(exit_code=result.returncode, command=cmd,
            log_sha256=sha(work / (variant + '-build.log')),
            module_sha256=sha(tree / 'go/go.mod'), sum_sha256=sha(tree / 'go/go.sum'),
            driver_source_sha256=sha(tree / 'go/pkg/driver.go'))
        if result.returncode == 0:
            info['builds'][variant].update(binary_sha256=sha(binary), private_build_info=subprocess.check_output(
                ['go', 'version', '-m', str(binary)], env=env, text=True))
        (work / 'manifest-private.json').write_text(json.dumps(info, indent=2) + '\n')
        assert result.returncode == 0, f'{variant} build failed; preserve logs and choose a new work directory'
    print('Both variants built. Review private manifest before running separate correctness/timing processes.')


if __name__ == '__main__':
    main()
