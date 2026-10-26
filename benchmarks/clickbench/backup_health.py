"""Explicit cluster backup preconditions, including useful failure evidence."""


def check_primary(client, index, expected_rows, primary):
    reply = client.execute_command('FT.INFO', index)
    info = dict(zip(reply[::2], reply[1::2]))
    actual = int(info[b'num_docs'])
    indexing = int(info[b'indexing'])
    if actual != expected_rows or indexing:
        raise RuntimeError(f'{primary}: index not ready: expected={expected_rows}, '
                           f'actual={actual}, indexing={indexing}')
    # INFO cluster only exposes cluster_enabled. Cluster state/slots belong
    # to CLUSTER INFO, including on a plain per-primary Redis connection.
    cluster = client.cluster('info')
    if cluster.get('cluster_state') != 'ok':
        raise RuntimeError(f'{primary}: CLUSTER INFO state={cluster.get("cluster_state")}')
    if client.info('persistence').get('loading'):
        raise RuntimeError(f'{primary}: snapshot is still loading')
