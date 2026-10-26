"""Validate a saved ClickBench topology and map it to new owned instances."""
import ipaddress


def restore_jobs(manifest, primaries):
    if not manifest.get('complete') or not manifest.get('restore_tested'):
        raise ValueError('A complete, restore-tested backup is required')
    nodes, files = manifest['nodes'], manifest['files']
    if len(nodes) != 48 or len(files) != 48 or len(primaries) != 48:
        raise ValueError('Exactly 48 saved and replacement primaries are required')
    ids = {n['node_id'] for n in nodes}
    addresses = [n['private_ip'] for n in primaries]
    if len(ids) != 48 or len(set(addresses)) != 48 or len({n['id'] for n in primaries}) != 48:
        raise ValueError('Duplicate Redis identity or replacement instance/address')
    if not all(ipaddress.ip_address(ip).is_private for ip in addresses):
        raise ValueError('Redis must use verified private addresses')
    mapping = dict(zip((n['node_id'] for n in nodes), primaries))
    snapshots = {f['node_id']: f for f in files}
    if set(snapshots) != ids or sum(f['dbsize'] for f in files) < manifest['expected_rows']:
        raise ValueError('Snapshot identity or row coverage differs')
    expected_slots = None
    jobs = []
    for saved in nodes:
        lines, slots, seen, myself, epochs = [], {}, set(), [], []
        for line in saved['cluster_nodes'].splitlines():
            parts = line.split()
            if len(parts) < 8 or parts[0] not in ids or parts[0] in seen:
                raise ValueError('Invalid or duplicate topology member')
            seen.add(parts[0])
            flags = set(parts[2].split(','))
            if flags - {'myself', 'master'} or 'master' not in flags or parts[3] != '-':
                raise ValueError('Only healthy primary topology is supported')
            if 'myself' in flags:
                myself.append(parts[0])
            if parts[7] != 'connected':
                raise ValueError('Saved topology has a disconnected member')
            epochs.append(int(parts[6]))
            for token in parts[8:]:
                bounds = token.split('-')
                if len(bounds) > 2 or not all(b.isdigit() for b in bounds):
                    raise ValueError('Migration markers or invalid slot range')
                start, stop = int(bounds[0]), int(bounds[-1])
                if not 0 <= start <= stop < 16384:
                    raise ValueError('Slot range is out of bounds')
                for slot in range(start, stop + 1):
                    if slot in slots:
                        raise ValueError('Duplicate slot ownership')
                    slots[slot] = parts[0]
            parts[1] = mapping[parts[0]]['private_ip'] + ':6379@16379'
            parts[4:6] = ['0', '0']
            lines.append(' '.join(parts))
        if seen != ids or myself != [saved['node_id']] or len(slots) != 16384:
            raise ValueError('Topology lacks exact membership, self identity or full slot coverage')
        if expected_slots is not None and slots != expected_slots:
            raise ValueError('Coordinators disagree about slot ownership')
        expected_slots = slots
        snapshot = snapshots[saved['node_id']]
        if snapshot['dbsize'] != saved['dbsize']:
            raise ValueError('Snapshot DBSIZE differs from saved node')
        lines.append(f'vars currentEpoch {max(epochs)} lastVoteEpoch 0')
        jobs.append({'instance': mapping[saved['node_id']], 'snapshot': snapshot,
                     'nodes_conf': '\n'.join(lines) + '\n', 'image': manifest['image']})
    return jobs
