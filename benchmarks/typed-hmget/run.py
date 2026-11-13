"""Opt-in reproduction in fresh private worktrees; never patches shipping code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

HERE = Path(__file__).resolve().parent
DATA = HERE / '2026-11-13'
ROOT = HERE.parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('parser', 'native'))
    parser.add_argument('--upstream-repo', type=Path, required=True, help='Existing official go-redis Git clone with the recorded base commits')
    parser.add_argument('--driver-repo', type=Path, default=ROOT)
    parser.add_argument('--source', type=Path, help='Exact independently typed Arrow input; native only')
    parser.add_argument('--work', type=Path, required=True, help='New private directory, never reused')
    parser.add_argument('--dedicated-empty-server', action='store_true')
    args = parser.parse_args()
    args.work = args.work.resolve()
    args.work.mkdir(mode=0o700, parents=True, exist_ok=False)
    assert subprocess.run(['git', 'rev-parse', '--show-toplevel'], cwd=args.work, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0, 'Use a private work directory outside an existing Git worktree'
    data = json.loads((DATA / 'results.json').read_text())
    sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
    for name, expected in data['patch_sha256'].items():
        assert sha(DATA / 'patches' / name) == expected
    env = os.environ.copy()
    for key in list(env):
        if key in ('GOFLAGS', 'GOWORK', 'GOMAXPROCS') or args.mode == 'parser' and (key.startswith(('REDIS_', 'GOREDIS_', 'SOFTWARE_', 'COMPOSE_')) or key == 'SSL_CERT_FILE'):
            env.pop(key)
    env.update(GOTOOLCHAIN='go1.27.3', GOWORK='off')
    counter = 0

    def run(command, cwd, extra=None):
        nonlocal counter
        counter += 1
        with (args.work / f'command-{counter:02d}.log').open('w') as log:
            result = subprocess.run(command, cwd=cwd, env={**env, **(extra or {})}, stdout=log, stderr=subprocess.STDOUT)
        assert result.returncode == 0, f'Command {counter} failed; preserve its private log'

    def checkout(repository, commit, name, patch=None):
        path = args.work / name
        run(['git', 'worktree', 'add', '--detach', str(path), commit], repository.resolve())
        if patch is not None:
            run(['git', 'apply', '--check', str(patch)], path)
            run(['git', 'apply', str(patch)], path)
        return path

    label = 'master' if args.mode == 'parser' else 'pinned'
    upstream = data['freeze']['upstream'][label]
    api = checkout(args.upstream_repo, upstream['base_commit'], 'go-redis-patched', DATA / 'patches' / ('upstream-master.patch' if label == 'master' else 'upstream-v9.23.patch'))
    for name, expected in upstream['files_sha256'].items():
        assert (not (api / name).exists()) if expected is None else sha(api / name) == expected
    if args.mode == 'parser':
        run(['go', 'test', '-count=3', '-p', '1', '-run', '^$', '-bench', '^BenchmarkHMGetStringsDecode$', '-benchmem', '-benchtime=200ms', '.'], api, {'GOMAXPROCS': '1'})
        print('Parser reproduction completed; all raw output remains in the private work directory.')
        return
    assert args.dedicated_empty_server and args.source is not None
    assert sha(args.source) == data['source_sha256']
    for name in ('REDIS_BENCH_URI', 'SSL_CERT_FILE', 'REDIS_BENCH_API_URL', 'REDIS_BENCH_API_USER', 'REDIS_BENCH_API_PASSWORD'):
        assert env.get(name), f'Require privately configured owned Software fixture variable {name}'
    baseline = checkout(args.driver_repo, data['native']['baseline']['build']['driver_base_commit'], 'baseline-driver')
    candidate = checkout(args.driver_repo, data['freeze']['driver_base_commit'], 'candidate-driver')
    overlay_tree = args.work / 'adapter'
    for name in data['freeze']['overlay_sources_sha256']:
        original = candidate / name
        if original.exists():
            destination = overlay_tree / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, destination)
    run(['git', 'apply', '--check', str(DATA / 'patches/driver-adapter.patch')], overlay_tree)
    run(['git', 'apply', str(DATA / 'patches/driver-adapter.patch')], overlay_tree)
    replacement = {}
    for name, expected in data['freeze']['overlay_sources_sha256'].items():
        assert sha(overlay_tree / name) == expected
        replacement[str(candidate / name)] = str(overlay_tree / name)
    overlay = args.work / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': replacement}))
    modfile = args.work / 'typed.mod'
    modfile.write_text((candidate / 'go/go.mod').read_text().rstrip() + '\n\nreplace github.com/redis/go-redis/v9 => ' + str(api) + '\n')
    shutil.copyfile(candidate / 'go/go.sum', args.work / 'typed.sum')
    normalized_mod = modfile.read_text().replace(str(api), '../go-redis-patched')
    assert hashlib.sha256(normalized_mod.encode()).hexdigest() == data['reproduction_hashes']['normalized_modfile_sha256']
    manifest = {
        'api_base_commit': upstream['base_commit'],
        'api_recorded_feature_commit': upstream['commit'],
        'api_applied_patch_sha256': data['patch_sha256']['upstream-v9.23.patch'],
        'api_source_sha256': {name: sha(api / name) if expected is not None else None for name, expected in upstream['files_sha256'].items()},
        'driver_base_commit': data['freeze']['driver_base_commit'],
        'adapter_source_sha256': {name: sha(overlay_tree / name) for name in data['freeze']['overlay_sources_sha256']},
        'adapter_patch_sha256': sha(DATA / 'patches/driver-adapter.patch'),
        'actual_overlay_json_sha256': sha(overlay),
        'actual_modfile_sha256': sha(modfile),
        'actual_sumfile_sha256': sha(args.work / 'typed.sum'),
        'normalized_modfile_sha256': hashlib.sha256(normalized_mod.encode()).hexdigest(),
        'go_version': subprocess.check_output(['go', 'version'], env=env, text=True).strip(),
        'builds': {},
    }
    extension = 'dylib' if sys.platform == 'darwin' else 'so'
    # Finish both builds before either timed block, matching the measured plan.
    for label, source in (('baseline', baseline), ('candidate', candidate)):
        version = data['native'][label]['build']['version']
        flags = ['-modfile=' + str(modfile), '-overlay=' + str(overlay)] if label == 'candidate' else []
        linker = '-s -w -X github.com/adbc-drivers/driverbase-go/driverbase.infoDriverVersion=' + version
        build = source / 'go/build'
        build.mkdir(exist_ok=True)
        library = build / ('libadbc_driver_redis.' + extension)
        cli = build / 'redis-adbc'
        run(['go', 'build', *flags, '-buildmode=c-shared', '-tags', 'driverlib', '-ldflags', linker, '-o', str(library), './pkg'], source / 'go', {'CGO_ENABLED': '1'})
        run(['go', 'build', *flags, '-ldflags', linker, '-o', str(cli), './cmd/redis-adbc'], source / 'go', {'CGO_ENABLED': '0'})
        manifest['builds'][label] = {
            'base_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=source, text=True).strip(),
            'version': version, 'driver_sha256': sha(library), 'cli_sha256': sha(cli),
            'private_library_build_info': subprocess.check_output(['go', 'version', '-m', str(library)], env=env, text=True),
            'private_cli_build_info': subprocess.check_output(['go', 'version', '-m', str(cli)], env=env, text=True),
        }
    (args.work / 'reproduction-manifest-private.json').write_text(json.dumps(manifest, indent=2) + '\n')
    for label, source in (('baseline', baseline), ('candidate', candidate)):
        version = data['native'][label]['build']['version']
        library = source / 'go/build' / ('libadbc_driver_redis.' + extension)
        cli = source / 'go/build/redis-adbc'
        run([sys.executable, str(source / 'benchmarks/table-modes/managed_read_memory.py'), '--source', str(args.source.resolve()), '--driver', str(library), '--cli', str(cli), '--version', version, '--work', str(args.work / (label + '-results')), '--dedicated-empty-server', '--read-repeats', '6'], source)
    print('Both native blocks completed; preserve all samples and record new build/module/overlay hashes.')


if __name__ == '__main__':
    main()
