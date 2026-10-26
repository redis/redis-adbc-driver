"""All-primary ACL management for an explicitly declared local TLS fixture."""
import json
from pathlib import Path
from urllib.parse import urlsplit, unquote

import redis


def declared_primaries(slots, ports):
    if len(ports) != 3 or len(set(ports)) != 3:
        raise ValueError('fixture must declare three distinct primary ports')
    nodes = {}
    for _, _, primary, *_ in slots:
        host, port, node_id = primary[:3]
        if host not in ('127.0.0.1', 'localhost') or port not in ports:
            raise ValueError('cluster advertises an endpoint outside the declared local fixture')
        if port in nodes and nodes[port] != node_id:
            raise ValueError('ambiguous primary node identity')
        nodes[port] = node_id
    if set(nodes) != set(ports) or len(set(nodes.values())) != 3:
        raise ValueError('fixture must expose three distinct primary owners')
    return nodes


class ClusterPrincipals:
    def __init__(self, control, fixture, admin_uri, ca):
        self.directory = Path(fixture)
        self.config = json.loads((self.directory / 'cluster.json').read_text())
        endpoint = urlsplit(admin_uri)
        if (self.config['deployment'] != 'owned OSS Cluster TLS' or
                endpoint.hostname not in ('localhost', '127.0.0.1') or
                endpoint.port != self.config['ports'][0]):
            raise ValueError('requires the declared owned loopback TLS fixture')
        self.nodes = declared_primaries(control.execute_command('CLUSTER', 'SLOTS'), self.config['ports'])
        self.clients, self.names = [], []
        self.ca = ca
        try:
            self.clients = self.clients_for_uri(admin_uri)
            for port, client in zip(sorted(self.nodes), self.clients):
                if client.execute_command('CLUSTER', 'MYID') != self.nodes[port]:
                    raise ValueError('primary node identity changed')
                if 'cluster_state:ok' not in client.execute_command('CLUSTER', 'INFO').splitlines():
                    raise ValueError('every primary must be ready')
                if client.config_get('search-max-aggregate-results') != {'search-max-aggregate-results': '137'}:
                    raise ValueError('every primary must have real aggregate cap 137')
                client.execute_command('FT._LIST')
            self.config['primary_node_ids'] = self.nodes
        except BaseException:
            for client in self.clients:
                client.close()
            raise

    def clients_for_uri(self, uri):
        endpoint = urlsplit(uri)
        return [redis.Redis(host='127.0.0.1', port=port,
            username=unquote(endpoint.username), password=unquote(endpoint.password),
            ssl=True, ssl_ca_certs=str(self.ca), ssl_cert_reqs='required',
            decode_responses=True, protocol=3, socket_timeout=10)
            for port in sorted(self.nodes)]

    def create(self, name, password, acl):
        if any(client.acl_getuser(name) is not None for client in self.clients):
            raise ValueError('refusing to adopt an existing cluster principal')
        # Register before the first write so partial creation can be cleaned up.
        self.names.append(name)
        for client in self.clients:
            client.execute_command('ACL', 'SETUSER', name, 'reset', 'on', '>' + password, *acl)

    def command(self, name, permission):
        if name not in self.names:
            raise ValueError('principal is not owned by this run')
        for client in self.clients:
            client.execute_command('ACL', 'SETUSER', name, permission)

    def cleanup(self):
        errors = []
        try:
            for index, client in enumerate(self.clients):
                try:
                    if self.names:
                        client.acl_deluser(*self.names)
                        if any(client.acl_getuser(name) is not None for name in self.names):
                            raise RuntimeError('owned principal remains')
                except Exception:
                    errors.append(index)
            record = {'status':'failed' if errors else 'passed', 'owned_names':self.names,
                      'primary_count':len(self.clients), 'failed_primary_indexes':errors,
                      'absence_verified_on_all_primaries':not errors}
            (self.directory / 'cluster-acl-cleanup.json').write_text(json.dumps(record,indent=2)+'\n')
            if errors:
                raise RuntimeError('owned cluster ACL cleanup failed on primary indexes: ' + str(errors))
        finally:
            for client in self.clients:
                client.close()
