"""Ensure semantic evidence corruption is rejected even with refreshed hashes."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import audit


class EvidenceAuditTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.bundle = Path(self.temp.name) / 'bundle'
        shutil.copytree(audit.BUNDLE, self.bundle)
        self.patch = patch.object(audit, 'BUNDLE', self.bundle)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def corrupt(self, name, mutation):
        path = self.bundle / name
        data = json.loads(path.read_text())
        mutation(data)
        path.write_text(json.dumps(data))
        manifest = {str(path.relative_to(self.bundle)): hashlib.sha256(path.read_bytes()).hexdigest()
                    for path in self.bundle.rglob('*') if path.is_file() and path.name != 'sha256.json'}
        (self.bundle / 'sha256.json').write_text(json.dumps(manifest))
        with self.assertRaises(AssertionError):
            audit.audit()

    def test_complete_evidence(self):
        with contextlib.redirect_stdout(io.StringIO()):
            audit.audit()

    def test_missing_warmup(self):
        self.corrupt('sql.json', lambda data: data['records'].pop(0))

    def test_changed_comparison(self):
        self.corrupt('comparison.json', lambda data: data['rows'][0]['candidate'].__setitem__('p50_ms', 1.0))

    def test_changed_harness_identity(self):
        self.corrupt('provenance.json', lambda data: data['sources']['candidate'].__setitem__('profile_test_sha256', '0' * 64))

    def test_unvalidated_record(self):
        self.corrupt('sql.json', lambda data: data['records'][1].__setitem__('validated', False))


if __name__ == '__main__':
    unittest.main()
