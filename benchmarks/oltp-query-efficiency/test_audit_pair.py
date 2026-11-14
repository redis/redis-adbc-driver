import csv
import json
from pathlib import Path
import tempfile
import unittest

import audit_pair
import compare
from test_compare import fixture


def save(bundle, name, data):
    path = bundle / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + '\n')


def seal(bundle):
    files = [p for p in bundle.rglob('*') if p.is_file() and p != bundle / 'sha256.json']
    save(bundle, 'sha256.json', {str(p.relative_to(bundle)): audit_pair.sha256(p) for p in files})


def create_bundle(bundle):
    (bundle / 'harness').mkdir()
    for name in ('run.py', 'scenarios.py'):
        (bundle / 'harness' / name).write_text('# frozen unit fixture: ' + name + '\n')
    harness = {name: audit_pair.sha256(bundle / 'harness' / name) for name in ('run.py', 'scenarios.py')}
    before, after = fixture(), fixture()
    for build, data in (('baseline', before), ('candidate', after)):
        data['metadata'].update(driver_version='v1-' + build, harness_sha256=harness['scenarios.py'])
    after['metadata'].update(driver_source_commit='d'*40, driver_sha256='e'*64)
    for name, data in (('baseline.json', before), ('candidate.json', after)):
        save(bundle, name, data)
    rows = compare.compare(before, after)
    document = {'rows': rows, 'source': {'baseline': before['metadata'], 'candidate': after['metadata']},
                'settings': compare.SETTINGS,
                'metric': 'Pooled operation latency after one warmup trial; three measured trials. p95 nearest rank.',
                'scope': 'Persistent application clients; execute plus full typed Arrow fetch or acknowledged mutation. Internal driver setup is included. Setup, independent validation and teardown excluded.',
                'input_sha256': {build: audit_pair.sha256(bundle / (build + '.json')) for build in ('baseline', 'candidate')}}
    save(bundle, 'comparison.json', document)
    with (bundle / 'comparison.csv').open('w', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0])); writer.writeheader(); writer.writerows(rows)
    identities = {build: {'commit': data['metadata']['driver_source_commit'],
                          'library_sha256': data['metadata']['driver_sha256'],
                          'cli_sha256': ('f' if build == 'baseline' else '0')*64,
                          'harness_sha256': harness} for build, data in (('baseline', before), ('candidate', after))}
    save(bundle, 'provenance.json', {'driver_builds': identities,
         'build_artifacts_after_run': {build: {key: identity[key] for key in ('library_sha256', 'cli_sha256')} for build, identity in identities.items()},
         'compare_source_sha256': audit_pair.sha256(Path(compare.__file__)),
         'post_measurement_production_changes': False, 'automatic_retries': 0,
         'failed_native_trial_records': 0, 'discarded_native_trial_records': 0})
    save(bundle, 'checks.json', {'final-race': {'source_commit': '9'*40, 'exit_code': 0,
         'passed_top_level_tests': 2, 'failed_top_level_tests': 0,
         'skipped_top_level_tests': 1, 'skipped_tests': ['TestOptionalTopology']}})
    # Stable fixtures describe actual archived settings; identities remain
    # synthetic and independently exercise different checkpoint/build sources.
    historical = Path(__file__).parent / '2026-11-13'
    for name in ('environment.json', 'cleanup.json'):
        (bundle / name).write_bytes((historical / name).read_bytes())
    (bundle / 'report.md').write_text('Synthetic unit fixture; no performance claim.\n')
    seal(bundle)


class PairAuditTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.bundle = Path(self.temp.name) / 'pair'; self.bundle.mkdir()
        create_bundle(self.bundle)

    def edit(self, name, change):
        data = json.loads((self.bundle / name).read_text()); change(data); save(self.bundle, name, data)
        seal(self.bundle)

    def test_complete_pair_retains_actual_checkpoint_sources(self):
        result = audit_pair.audit_bundle(self.bundle, ('final-race',))
        self.assertEqual((result['baseline_records'], result['candidate_records'], result['comparison_rows']), (168, 168, 42))
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle, ('missing-checkpoint',))

    def test_missing_record_and_inaccurate_chart_are_rejected(self):
        self.edit('baseline.json', lambda d: d['trials'].pop())
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)
        create = Path(self.temp.name) / 'other'; create.mkdir(); create_bundle(create)
        data = json.loads((create / 'comparison.json').read_text()); data['rows'][0]['p50_speedup'] = 2; save(create, 'comparison.json', data); seal(create)
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(create)

    def test_source_and_post_run_artifacts_must_match(self):
        for field in ('commit', 'library_sha256', 'cli_sha256'):
            with self.subTest(field=field):
                create_bundle_path = Path(self.temp.name) / field; create_bundle_path.mkdir(); create_bundle(create_bundle_path)
                document = json.loads((create_bundle_path / 'provenance.json').read_text())
                document['driver_builds']['baseline'][field] = '1'*(40 if field == 'commit' else 64)
                save(create_bundle_path, 'provenance.json', document); seal(create_bundle_path)
                with self.assertRaises(AssertionError): audit_pair.audit_bundle(create_bundle_path)

    def test_frozen_helper_harness_is_checked(self):
        (self.bundle / 'harness/run.py').write_text('# changed helper\n'); seal(self.bundle)
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)

    def test_failed_check_or_unrecorded_skip_is_rejected(self):
        self.edit('checks.json', lambda d: d['final-race'].update(skipped_top_level_tests=0))
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)

    def test_wrong_environment_or_incomplete_cleanup_is_rejected(self):
        self.edit('environment.json', lambda d: d.update(qpf=1))
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)
        save(self.bundle, 'environment.json', json.loads((Path(__file__).parent / '2026-11-13/environment.json').read_text()))
        self.edit('cleanup.json', lambda d: d.update(owned_container_and_network_removed=False))
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)

    def test_csv_and_manifest_are_complete(self):
        with (self.bundle / 'comparison.csv').open('a') as stream: stream.write('extra,row\n')
        seal(self.bundle)
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)

    def test_manifest_cannot_omit_or_escape_bundle(self):
        (self.bundle / 'unrecorded.txt').write_text('not in manifest')
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)
        (self.bundle / 'unrecorded.txt').unlink()
        self.edit('sha256.json', lambda d: d.update({'../outside': '0'*64}))
        # edit reseals; explicitly insert the prohibited path after sealing.
        manifest = json.loads((self.bundle / 'sha256.json').read_text()); manifest['../outside'] = '0'*64; save(self.bundle, 'sha256.json', manifest)
        with self.assertRaises(AssertionError): audit_pair.audit_bundle(self.bundle)


if __name__ == '__main__':
    unittest.main()
