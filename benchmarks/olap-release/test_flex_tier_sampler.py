"""The reference Flex object sampler: a fake opener, no management API or Redis."""

import base64
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import flex_tier_sampler


CONFIG = {"url": "https://localhost:9443", "username": "admin@example.com", "password": "private-secret",
          "bdb_uid": 2, "verify_tls": False}
# GET /v1/bdbs/stats/last/2 on the measured Software 8.2.6 Flex fixture (shape; values from the shard).
STATS = {"2": {"stime": "2026-11-26T18:43:26Z", "etime": "2026-11-26T18:43:27Z",
               "bigstore_objs_ram": 7803390.0, "bigstore_objs_flash": 8190741.0,
               "bigstore_objs_clean": 6938217.0, "bigstore_objs_dirty": 865173.0, "no_of_keys": 8661322.0,
               "used_memory": 13444720436.0, "used_bigstore": 6374828452.0, "big_fetch_flash": 17633.5,
               "conns": 32.0}}


class FakeOpener:
    def __init__(self, reply=STATS):
        self.reply, self.calls = reply, []

    def __call__(self, request, context=None, timeout=None):
        self.calls.append((request, context, timeout))
        return io.BytesIO(json.dumps(self.reply).encode())


class SampleTests(unittest.TestCase):
    def test_maps_the_latest_interval_to_the_hook_fields(self):
        opener = FakeOpener()
        self.assertEqual(flex_tier_sampler.sample(CONFIG, opener=opener), {
            "source": "management_api_bdb_stats", "ram_objects": 7803390, "flash_objects": 8190741,
            "ram_clean_objects": 6938217, "ram_dirty_objects": 865173, "keys": 8661322,
            "used_memory_bytes": 13444720436, "used_flash_bytes": 6374828452,
            "interval_start_utc": "2026-11-26T18:43:26Z", "interval_end_utc": "2026-11-26T18:43:27Z"})
        (request, context, timeout), = opener.calls
        self.assertEqual(request.full_url, "https://localhost:9443/v1/bdbs/stats/last/2")
        self.assertEqual(request.get_method(), "GET")
        scheme, token = request.get_header("Authorization").split()
        self.assertEqual((scheme, base64.b64decode(token).decode()), ("Basic", "admin@example.com:private-secret"))
        self.assertEqual(context.verify_mode, ssl.CERT_NONE)
        self.assertEqual(timeout, flex_tier_sampler.TIMEOUT_SECONDS)

    def test_non_integral_missing_and_foreign_values_are_dropped(self):
        stats = {"2": {"bigstore_objs_ram": 1.5, "bigstore_objs_flash": -3.0, "no_of_keys": True,
                       "used_memory": "13444720436", "etime": 1795717265}}
        self.assertEqual(flex_tier_sampler.sample(CONFIG, opener=FakeOpener(stats)),
                         {"source": "management_api_bdb_stats"})
        for reply in ({"3": STATS["2"]}, [], {"2": []}):
            with self.subTest(reply=reply), self.assertRaises(ValueError):
                flex_tier_sampler.sample(CONFIG, opener=FakeOpener(reply))

    def test_configuration_and_tls_rules(self):
        default = flex_tier_sampler.request({**CONFIG, "verify_tls": True})[1]
        self.assertEqual((default.verify_mode, default.check_hostname), (ssl.CERT_REQUIRED, True))
        for host in ("localhost", "127.0.0.1", "[::1]"):
            context = flex_tier_sampler.request({**CONFIG, "url": f"https://{host}:9443"})[1]
            self.assertEqual(context.verify_mode, ssl.CERT_NONE)
        bad = [{"url": "https://cluster.example.com:9443"},  # verify_tls false off localhost
               {"url": "http://localhost:9443"}, {"url": "https://admin:private-secret@localhost:9443"},
               {"url": "https://localhost:9443/v1"}, {"bdb_uid": "2"}, {"bdb_uid": 0}, {"bdb_uid": True},
               {"verify_tls": "false"}, {"password": None}, {"username": 7}]
        for change in bad:
            with self.subTest(change=change):
                opener = FakeOpener()
                with self.assertRaises(ValueError):
                    flex_tier_sampler.sample({**CONFIG, **change}, opener=opener)
                self.assertEqual(opener.calls, [])


class MainTests(unittest.TestCase):
    def test_prints_one_object_on_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "management.json"
            path.write_text(json.dumps(CONFIG))
            out = io.StringIO()
            with patch.object(flex_tier_sampler, "sample", lambda config: {"ram_objects": 1}), redirect_stdout(out):
                self.assertEqual(flex_tier_sampler.main(["flex_tier_sampler.py", str(path)]), 0)
            self.assertEqual(json.loads(out.getvalue()), {"ram_objects": 1})

    def test_failure_prints_nothing_and_never_the_credentials(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "management.json"
            # Nothing listens on port 1: a real connection failure, without a server.
            path.write_text(json.dumps({**CONFIG, "url": "https://127.0.0.1:1"}))
            script = Path(flex_tier_sampler.__file__)
            done = subprocess.run([sys.executable, str(script), str(path)], capture_output=True, text=True,
                                  timeout=60)
            self.assertEqual(done.returncode, 1)
            self.assertEqual(done.stdout, "")
            self.assertIn("flex_tier_sampler:", done.stderr)
            self.assertNotIn("private-secret", done.stderr)
            self.assertNotIn("admin@example.com", done.stderr)
            usage = subprocess.run([sys.executable, str(script)], capture_output=True, text=True, timeout=60)
            self.assertEqual((usage.returncode, usage.stdout), (2, ""))


if __name__ == "__main__":
    unittest.main()
