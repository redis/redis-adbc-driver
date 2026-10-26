import copy
import unittest
from restored_cluster import restore_jobs


def saved_topology():
    """A complete synthetic backup with uneven, contiguous slot assignments."""
    ids = [f'{i + 1:040x}' for i in range(48)]
    nodes = []
    for owner in ids:
        lines = []
        for i, node_id in enumerate(ids):
            start, stop = i * 16384 // 48, (i + 1) * 16384 // 48 - 1
            flags = 'myself,master' if node_id == owner else 'master'
            lines.append(f'{node_id} 10.20.1.{i + 1}:6379@16379 {flags} - 0 0 {i + 1} connected {start}-{stop}')
        nodes.append({'node_id': owner, 'dbsize': 100, 'cluster_nodes': '\n'.join(lines)})
    return {'complete': True, 'restore_tested': True, 'expected_rows': 4800,
            'image': 'redis@sha256:fixture', 'nodes': nodes,
            'files': [{'node_id': node_id, 'dbsize': 100} for node_id in ids]}


class RestoreMappingTests(unittest.TestCase):
    def setUp(self):
        self.manifest = saved_topology()
        self.primaries = [{'id': f'test-{i}', 'private_ip': f'10.40.1.{i + 1}'} for i in range(48)]

    def test_complete_saved_topology_maps_exactly(self):
        jobs = restore_jobs(self.manifest, self.primaries)
        self.assertEqual(len(jobs), 48)
        self.assertEqual({j['snapshot']['node_id'] for j in jobs},
                         {n['node_id'] for n in self.manifest['nodes']})
        for job in jobs:
            self.assertIn(job['instance']['private_ip'] + ':6379@16379 myself,master', job['nodes_conf'])
            self.assertIn('vars currentEpoch ', job['nodes_conf'])

    def test_reject_public_duplicate_or_missing_replacement(self):
        for kind in ['public', 'duplicate', 'missing']:
            with self.subTest(kind=kind):
                p = copy.deepcopy(self.primaries)
                if kind == 'public': p[0]['private_ip'] = '8.8.8.8'
                elif kind == 'duplicate': p[1] = p[0]
                else: p.pop()
                with self.assertRaises(ValueError): restore_jobs(self.manifest, p)

    def test_reject_missing_or_duplicate_slots(self):
        for token in ['', ' 0']:
            m = copy.deepcopy(self.manifest)
            lines = m['nodes'][0]['cluster_nodes'].splitlines()
            lines[0] = ' '.join(lines[0].split()[:8]) if not token else lines[0] + token
            m['nodes'][0]['cluster_nodes'] = '\n'.join(lines)
            with self.assertRaises(ValueError): restore_jobs(m, self.primaries)

    def test_reject_incomplete_backup(self):
        m = copy.deepcopy(self.manifest); m['complete'] = False
        with self.assertRaises(ValueError): restore_jobs(m, self.primaries)

    def test_reject_wrong_snapshot_identity(self):
        m = copy.deepcopy(self.manifest); m['files'][0]['node_id'] = 'wrong'
        with self.assertRaises(ValueError): restore_jobs(m, self.primaries)


if __name__ == '__main__': unittest.main()
