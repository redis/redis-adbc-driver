"""Exercise the actual workflow shell loops without network or package changes."""
from pathlib import Path
import re
import subprocess
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[1]
SOURCES = (
    '.github/actions/duckdb-build-cache/action.yml',
    '.github/workflows/release.yml',
)


class AptRetryTests(unittest.TestCase):
    def run_loop(self, source, failure, transient):
        content = (ROOT / source).read_text()
        loop = re.search(r'(?m)^([ ]*)for attempt in 1 2 3; do\n.*?^\1done$',
                         content, re.DOTALL)
        self.assertIsNotNone(loop, source)
        with tempfile.TemporaryDirectory() as directory:
            script = r'''
set -eu
sudo() { "$@"; }
sleep() { :; }
apt-get() {
    case " $* " in
        *" update "*) operation=update ;;
        *" install "*) operation=install ;;
        *) exit 90 ;;
    esac
    echo "$operation" >> calls
    if [ "$operation" = update ]; then
        case " $* " in
            *" APT::Update::Error-Mode=any "*) ;;
            *) exit 91 ;;
        esac
    fi
    if [ "$operation" = "$FAILURE" ]; then
        if [ "$TRANSIENT" = no ] || [ ! -f failed ]; then
            touch failed
            return 1
        fi
    fi
}
'''
            result = subprocess.run(
                ['bash', '-c', script + textwrap.dedent(loop.group())],
                cwd=directory, env={'FAILURE': failure, 'TRANSIENT': transient},
                capture_output=True, text=True, timeout=5,
            )
            self.assertTrue((Path(directory) / 'calls').exists(), result.stderr)
            calls = (Path(directory) / 'calls').read_text().splitlines()
            return result.returncode, calls

    def test_success(self):
        for source in SOURCES:
            with self.subTest(source=source):
                self.assertEqual(self.run_loop(source, '', 'no'),
                                 (0, ['update', 'install']))

    def test_transient_update_never_installs_from_failed_index(self):
        for source in SOURCES:
            with self.subTest(source=source):
                self.assertEqual(self.run_loop(source, 'update', 'yes'),
                                 (0, ['update', 'update', 'install']))

    def test_transient_install_retries_transaction(self):
        for source in SOURCES:
            with self.subTest(source=source):
                self.assertEqual(self.run_loop(source, 'install', 'yes'),
                                 (0, ['update', 'install'] * 2))

    def test_permanent_failure_is_bounded_and_fatal(self):
        for source in SOURCES:
            for failure in ('update', 'install'):
                with self.subTest(source=source, failure=failure):
                    status, calls = self.run_loop(source, failure, 'no')
                    self.assertEqual(status, 1)
                    self.assertEqual(calls, (['update'] if failure == 'update'
                                             else ['update', 'install']) * 3)


if __name__ == '__main__':
    unittest.main()
