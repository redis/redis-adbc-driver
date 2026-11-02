"""Exercise bootstrap API retry behavior against a local transient HTTP endpoint."""
import http.server
from pathlib import Path
import shutil
import subprocess
import threading
import unittest


@unittest.skipUnless(shutil.which('curl'), 'curl is required by software-setup.sh')
class BootstrapHTTPTests(unittest.TestCase):
    def test_transient_reads_and_idempotent_puts_retry_but_post_does_not(self):
        counts = {}

        class Handler(http.server.BaseHTTPRequestHandler):
            def respond(self):
                key = self.command
                counts[key] = counts.get(key, 0) + 1
                self.send_response(503 if counts[key] == 1 else 200)
                self.end_headers()
                self.wfile.write(b'{}')

            do_GET = do_PUT = do_POST = respond

            def log_message(self, *args):
                pass

        source = (Path(__file__).resolve().parents[1] / 'go/software-setup.sh').read_text()
        start = source.index('call() {')
        call = source[start:source.index('\nwait_for()', start)]
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f'http://127.0.0.1:{server.server_port}/v1/test'
            for method in ('GET', 'PUT', 'POST'):
                script = 'set -euo pipefail\nadmin_user=test\nadmin_pass=test\n' + call
                script += f'\ncall -f -X {method} "$1"\n'
                result = subprocess.run(['bash', '-c', script, 'bootstrap-test', url],
                                        capture_output=True, text=True, timeout=10)
                with self.subTest(method=method):
                    self.assertEqual(result.returncode, 22 if method == 'POST' else 0, result.stderr)
                    self.assertEqual(counts[method], 1 if method == 'POST' else 2)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__':
    unittest.main()
