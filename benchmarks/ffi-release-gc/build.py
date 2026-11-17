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
import sys
import tarfile

BASE = '24f654b76b98f3d8960af94a3ca0230185d467a1'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    if sys.version_info < (3, 14):
        raise SystemExit('Use Python 3.14 or newer for native probe builds')
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--work', type=Path, required=True)
    p.add_argument('--race', action='store_true', help='Separate correctness binaries; never use for timing')
    p.add_argument('--base', default=BASE, help='Source commit to archive; historical default remains unchanged')
    p.add_argument('--baseline-only', action='store_true', help='Ownership gate on unchanged GC policy; do not build an omission candidate')
    p.add_argument('--checkptr', action='store_true', help='Compile all packages with strict checkptr=2')
    p.add_argument('--cgo-check', action='store_true', help='Build with GOEXPERIMENT=cgocheck2')
    p.add_argument('--sanitizer', choices=('address', 'undefined'), help='Separate native lifetime binaries, never timing variants')
    p.add_argument('--arrow-source', type=Path, help='Explicit nonshipping Arrow dependency experiment in the private archived tree only')
    args = p.parse_args()
    if args.race and args.sanitizer:
        p.error('Use separate race and sanitizer builds')
    repo = Path(__file__).resolve().parents[2]
    work = args.work.resolve()
    assert not work.exists(), 'Use a new private directory; preserve earlier attempts'
    assert not subprocess.run(['git', '-C', str(work.parent), 'rev-parse', '--show-toplevel'],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 'Private work directory must be outside a Git checkout'
    work.mkdir()
    env = os.environ.copy()
    for key in ('GOFLAGS', 'GOWORK', 'GOMAXPROCS', 'GOEXPERIMENT', 'GOTOOLCHAIN', 'REDIS_URI', 'REDIS_SNAPSHOT_URI'):
        env.pop(key, None)
    # Follow the archived source's module toolchain, including historical bases.
    env.update(GOWORK='off', GOTOOLCHAIN='auto', CGO_ENABLED='1')
    if args.cgo_check:
        env['GOEXPERIMENT'] = 'cgocheck2'
    if args.sanitizer == 'undefined':
        for name in ('CGO_CFLAGS', 'CGO_CXXFLAGS'):
            env[name] = env.get(name, '') + ' -fsanitize=undefined -fno-sanitize-recover=all -fno-omit-frame-pointer'
        env['CGO_LDFLAGS'] = env.get('CGO_LDFLAGS', '') + ' -fsanitize=undefined'
    base = subprocess.check_output(['git', 'rev-parse', '--verify', args.base + '^{commit}'], cwd=repo, text=True).strip()
    archive = subprocess.check_output(['git', 'archive', base, 'go'], cwd=repo)
    source = Path(__file__).with_name('probe.go.txt')
    patch = Path(__file__).parent / 'patches/omit-forced-gc.patch'
    arrow_override = None
    if args.arrow_source:
        arrow_source = args.arrow_source.resolve(strict=True)
        assert 'module github.com/apache/arrow-go/v18' in (arrow_source / 'go.mod').read_text()
        digest = hashlib.sha256()
        for path in sorted(arrow_source.rglob('*')):
            if path.is_file():
                digest.update(str(path.relative_to(arrow_source)).encode() + b'\0')
                digest.update(bytes.fromhex(sha(path)))
        arrow_override = dict(nonshipping_dependency_override=True,
                              source_tree_sha256=digest.hexdigest(),
                              cdata_exports_sha256=sha(arrow_source / 'arrow/cdata/cdata_exports.go'))
    info = dict(base_commit=base, baseline_only=args.baseline_only, archive_sha256=hashlib.sha256(archive).hexdigest(),
                fixture_sha256=sha(source), patch_sha256=sha(patch), race=args.race,
                checkptr=args.checkptr, cgo_check=args.cgo_check, sanitizer=args.sanitizer,
                arrow_override=arrow_override,
                go_launcher=subprocess.check_output(['go', 'version'], env=env, text=True).strip(), builds={})
    for variant in (('baseline',) if args.baseline_only else ('baseline', 'candidate')):
        tree = work / (variant + '-source')
        tree.mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(tree, filter='data')
        if args.arrow_source:
            subprocess.run(['go', 'mod', 'edit', '-replace=github.com/apache/arrow-go/v18=' + str(arrow_source)],
                           cwd=tree / 'go', env=env, check=True)
        fixture = source.read_bytes()
        # Historical bridges donated the imported reference. Current bridges
        # release it after BindStream, matching Go ADBC's retaining contract.
        # Adapt only the stub, never the archived production bridge, and retain
        # the actual overlaid fixture hash separately from the historical file.
        retains_stream = 'defer rdr.(array.RecordReader).Release()' in (tree / 'go/pkg/driver.go').read_text()
        if retains_stream:
            marker = b'func (s *probeStatement) BindStream(_ context.Context, r array.RecordReader) error {\n'
            assert fixture.count(marker) == 1
            fixture = fixture.replace(marker, marker + b'\tif r != nil { r.Retain() }\n')
        (tree / 'go/pkg/ffi_release_probe.go').write_bytes(fixture)
        if variant == 'candidate':
            subprocess.run(['git', 'apply', '--check', str(patch)], cwd=tree, check=True)
            subprocess.run(['git', 'apply', str(patch)], cwd=tree, check=True)
        info['go'] = subprocess.check_output(['go', 'version'], cwd=tree / 'go', env=env, text=True).strip()
        ext = {'darwin': 'dylib', 'linux': 'so', 'windows': 'dll'}[subprocess.check_output(
            ['go', 'env', 'GOOS'], cwd=tree / 'go', env=env, text=True).strip()]
        binary = work / (variant + ('.race' if args.race else '') + '.' + ext)
        cmd = ['go', 'build', '-buildmode=c-shared', '-tags', 'driverlib,ffireleaseprobe',
               '-ldflags', '-s -w -X github.com/adbc-drivers/driverbase-go/driverbase.infoDriverVersion=v0.1.0-dev.ffi-release-gc',
               '-o', str(binary), './pkg']
        if args.race:
            cmd.insert(2, '-race')
        if args.checkptr:
            cmd.insert(2, '-gcflags=all=-d=checkptr=2')
        if args.sanitizer == 'address':
            cmd.insert(2, '-asan')
        with (work / (variant + '-build.log')).open('w') as log:
            result = subprocess.run(cmd, cwd=tree / 'go', env=env, stdout=log, stderr=subprocess.STDOUT)
        info['builds'][variant] = dict(exit_code=result.returncode, command=cmd,
            fixture_written_sha256=hashlib.sha256(fixture).hexdigest(), retains_stream=retains_stream,
            log_sha256=sha(work / (variant + '-build.log')),
            module_sha256=sha(tree / 'go/go.mod'), sum_sha256=sha(tree / 'go/go.sum'),
            driver_source_sha256=sha(tree / 'go/pkg/driver.go'))
        if result.returncode == 0:
            info['builds'][variant].update(binary_sha256=sha(binary), private_build_info=subprocess.check_output(
                ['go', 'version', '-m', str(binary)], env=env, text=True))
        (work / 'manifest-private.json').write_text(json.dumps(info, indent=2) + '\n')
        assert result.returncode == 0, f'{variant} build failed; preserve logs and choose a new work directory'
    print('Builds complete. Review private manifest before running separate correctness/timing processes.')


if __name__ == '__main__':
    main()
