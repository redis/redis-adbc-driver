#!/usr/bin/env python3
"""Start/stop one disposable native Redis/Search fixture without a brew service.

State, logs and version evidence live in a new caller-owned directory. Stop
checks the server PID before shutdown, rather than killing another Redis.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time


def command(state, *args):
    tls_options, env = [], os.environ.copy()
    if state.get('tls_directory'):
        directory = Path(state['tls_directory'])
        tls_options = ['--tls', '--cacert', str(directory / 'ca.crt')]
        env['REDISCLI_AUTH'] = (directory / 'admin.password').read_text().strip()
    return subprocess.check_output([
        state['cli'], '-h', '127.0.0.1', '-p', str(state['port']), '-e', *tls_options, *args,
    ], text=True, stderr=subprocess.STDOUT, timeout=5, env=env)


def fields(text):
    return dict(line.strip().split(':', 1) for line in text.splitlines()
                if line and not line.startswith('#') and ':' in line)


def start(args):
    if not 1 <= args.port <= 65535:
        raise ValueError('port must be in 1..65535')
    server, cli, module = [str(path.resolve(strict=True)) for path in (args.server, args.cli, args.module)]
    directory = args.directory.resolve()
    directory.mkdir(mode=0o700)
    tls_directory = directory / 'tls'
    config = []
    if args.tls:
        subprocess.run(['bash', str(Path(__file__).with_name('create_partition_tls.sh')),
                        str(tls_directory), str(args.port)], check=True)
        text = (tls_directory / 'redis.conf').read_text()
        for name in ('server.crt', 'server.key', 'ca.crt'):
            text = text.replace('/tls/' + name, json.dumps(str(tls_directory / name), ensure_ascii=False))
        text = text.replace('tls-port 6379', 'tls-port ' + str(args.port))
        native_config = directory / 'tls-redis.conf'
        native_config.touch(mode=0o600)
        native_config.write_text(text)
        config = [str(native_config)]
    with (directory / 'redis.log').open('w') as log:
        process = subprocess.Popen([
            server, *config, '--bind', '127.0.0.1', '--port', '0' if args.tls else str(args.port),
            '--save', '', '--appendonly', 'no', '--loadmodule', module,
            '--dir', str(directory),
        ], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
            start_new_session=True)
    state = {'pid': process.pid, 'port': args.port, 'cli': cli, 'server': server, 'module': module}
    if args.tls:
        state['tls_directory'] = str(tls_directory)
    try:
        (directory / 'state.json').write_text(json.dumps(state, indent=2) + '\n')
        deadline = time.monotonic() + 30
        while True:
            if process.poll() is not None:
                raise RuntimeError('native Redis exited; inspect redis.log')
            try:
                if command(state, 'PING').strip() == 'PONG':
                    break
            except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
                pass
            if time.monotonic() >= deadline:
                raise TimeoutError('native Redis did not become ready')
            time.sleep(0.1)
        # A pre-existing server on this port must never be reconfigured.
        info = command(state, 'INFO', 'server')
        if int(fields(info)['process_id']) != process.pid:
            raise RuntimeError('fixture port belongs to another process')
        command(state, 'FT._LIST')
        command(state, 'CONFIG', 'SET', 'search-max-aggregate-results', '137' if args.tls else '2147483648')
        (directory / 'server.info').write_text(info)
        (directory / 'modules.txt').write_text(command(state, 'MODULE', 'LIST'))
        (directory / 'uri').touch(mode=0o600)
        (directory / 'uri').write_text((tls_directory / 'admin.uri').read_text() if args.tls else f'redis://127.0.0.1:{args.port}/0\n')
        print('Native Redis/Search ready; isolated fixture, TLS cap 137.' if args.tls else 'Native Redis/Search ready; isolated local fixture, standard Go-test cap.')
    except BaseException:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        raise


def stop(directory):
    path = directory / 'state.json'
    if not path.exists():
        return
    state = json.loads(path.read_text())
    try:
        os.kill(state['pid'], 0)
    except ProcessLookupError:
        return
    info = fields(command(state, 'INFO', 'server'))
    if int(info['process_id']) != state['pid']:
        raise RuntimeError('fixture identity changed; refusing to shut down another server')
    command(state, 'SHUTDOWN', 'NOSAVE')
    print('Stopped the owned native Redis fixture.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    launch = sub.add_parser('start')
    launch.add_argument('directory', type=Path)
    launch.add_argument('--server', type=Path, required=True)
    launch.add_argument('--cli', type=Path, required=True)
    launch.add_argument('--module', type=Path, required=True)
    launch.add_argument('--port', type=int, required=True)
    launch.add_argument('--tls', action='store_true')
    shutdown = sub.add_parser('stop')
    shutdown.add_argument('directory', type=Path)
    args = parser.parse_args()
    if args.action == 'start':
        start(args)
    else:
        stop(args.directory)


if __name__ == '__main__':
    main()
